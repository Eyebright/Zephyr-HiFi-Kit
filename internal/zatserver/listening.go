package zatserver

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"hifihub/zat"
)

func (e *endpoint) acceptStatus(st zat.Status, now time.Time) {
	e.mu.Lock()
	e.alsaDevice, e.prefDevice, e.underruns = st.ALSADevice, st.PrefDevice, st.Underruns
	if len(st.ALSADevices) > 0 {
		e.caps.ALSADevices = st.ALSADevices
	}
	if len(st.ALSADeviceInfo) > 0 {
		e.caps.ALSADeviceInfo = st.ALSADeviceInfo
	}
	ps := e.play
	valid := ps != nil && st.SessionID == ps.sessionID && ps.ctx.Err() == nil
	if valid {
		e.pts, e.bufferMS, e.lastStatus, e.sentSince = st.PTS, st.BufferMS, now, 0
		e.state = zat.State(st.State)
	}
	e.mu.Unlock()
	if valid {
		if err := ps.listening.observe(e.srv.db, st, now); err != nil {
			slog.Warn("zat record listening", "node", e.nodeID, "err", err)
		}
	}
}

type listeningClock struct {
	mu        sync.Mutex
	historyID string
	rate      int
	enabled   bool
	lastAt    time.Time
	lastPTS   uint64
	playing   bool
	creditMS  int64
	pendingMS int64
}

func (c *listeningClock) start(id string, rate int, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.historyID, c.rate, c.enabled = id, rate, true
	c.lastAt, c.lastPTS, c.playing = now, 0, true
}

func (c *listeningClock) gate(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
	c.lastAt = time.Time{}
	c.playing = false
	c.creditMS, c.pendingMS = 0, 0
}

func (c *listeningClock) observe(db *sql.DB, st zat.Status, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || c.historyID == "" || c.rate <= 0 {
		return nil
	}
	at, pts, playing := c.lastAt, c.lastPTS, c.playing
	c.lastAt, c.lastPTS, c.playing = now, st.PTS, st.State == string(zat.StatePlaying)
	elapsed := now.Sub(at)
	if at.IsZero() || !playing || elapsed <= 0 || elapsed > 5*time.Second || st.PTS <= pts {
		c.creditMS, c.pendingMS = 0, 0
		return nil
	}
	if st.State != string(zat.StatePlaying) && st.State != string(zat.StateStopped) && st.State != string(zat.StatePaused) {
		c.creditMS, c.pendingMS = 0, 0
		return nil
	}
	// Reports can be jittery, but discontinuities and unobserved gaps are not listening.
	delta := st.PTS - pts
	maxSamples := uint64(elapsed+250*time.Millisecond) * uint64(c.rate) / uint64(time.Second)
	if delta > maxSamples {
		c.creditMS, c.pendingMS = 0, 0
		return nil
	}
	// Carry chunk/report quantization across ticks, bounded by both samples and wall time.
	c.creditMS += elapsed.Milliseconds()
	c.pendingMS += int64(delta) * 1000 / int64(c.rate)
	ms := min(c.pendingMS, c.creditMS)
	c.pendingMS -= ms
	c.creditMS -= ms
	if ms <= 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	from := now.Add(-time.Duration(ms) * time.Millisecond)
	for from.Before(now) {
		day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
		end := day.AddDate(0, 0, 1)
		if end.After(now) {
			end = now
		}
		part := end.Sub(from).Milliseconds()
		if part > 0 {
			_, err = tx.Exec(`INSERT INTO listen_daily(history_id,day,listen_ms,last_played_at) VALUES(?,?,?,?)
				ON CONFLICT(history_id,day) DO UPDATE SET listen_ms=listen_ms+excluded.listen_ms,
				last_played_at=MAX(last_played_at,excluded.last_played_at)`, c.historyID, day.Unix(), part, end.Unix())
			if err != nil {
				return err
			}
		}
		from = end
	}
	return tx.Commit()
}
