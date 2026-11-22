package zatserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Sleep timers stop a node's playback at a wall-clock deadline. They are
// persisted so a console reload or server restart does not lose them, and they
// belong to the node that was selected when set (switching terminals in the
// player does not move the deadline).

const (
	sleepTickMaxMinutes = 1440
	sleepCheckInterval  = 5 * time.Second
)

// ErrSleepMinutes is returned for out-of-range timer durations.
var ErrSleepMinutes = errors.New("定时时长需为 1..1440 分钟")

// loadSleepTimers restores persisted deadlines into memory at startup.
func (s *Server) loadSleepTimers() {
	rows, err := s.db.Query(`SELECT node_id,stop_at FROM node_sleep_timer`)
	if err != nil {
		slog.Warn("zat load sleep timers", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var stopAt int64
		if err := rows.Scan(&id, &stopAt); err != nil {
			slog.Warn("zat load sleep timers", "err", err)
			return
		}
		s.sleepStop[id] = stopAt
	}
}

// SetSleepTimer arms (or overwrites) a node's sleep timer and returns the
// unix deadline. Minutes are constrained to 1..1440.
func (s *Server) SetSleepTimer(nodeID string, minutes int) (int64, error) {
	if minutes < 1 || minutes > sleepTickMaxMinutes {
		return 0, ErrSleepMinutes
	}
	if _, err := s.nodes.Get(nodeID); err != nil {
		return 0, err
	}
	stopAt := time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	if _, err := s.db.Exec(`INSERT INTO node_sleep_timer(node_id,stop_at) VALUES(?,?)
		ON CONFLICT(node_id) DO UPDATE SET stop_at=excluded.stop_at`, nodeID, stopAt); err != nil {
		return 0, fmt.Errorf("保存定时失败：%w", err)
	}
	s.sleepMu.Lock()
	s.sleepStop[nodeID] = stopAt
	s.sleepMu.Unlock()
	slog.Info("zat sleep timer armed", "node", nodeID, "minutes", minutes, "stop_at", stopAt)
	return stopAt, nil
}

// CancelSleepTimer removes a node's timer; it is a no-op when none is set.
func (s *Server) CancelSleepTimer(nodeID string) error {
	if _, err := s.db.Exec(`DELETE FROM node_sleep_timer WHERE node_id=?`, nodeID); err != nil {
		return fmt.Errorf("取消失败：%w", err)
	}
	s.sleepMu.Lock()
	delete(s.sleepStop, nodeID)
	s.sleepMu.Unlock()
	return nil
}

// SleepStopAt returns the node's deadline, or 0 when no timer is set.
func (s *Server) SleepStopAt(nodeID string) int64 {
	s.sleepMu.Lock()
	defer s.sleepMu.Unlock()
	return s.sleepStop[nodeID]
}

func (s *Server) runSleepTimers(ctx context.Context) {
	t := time.NewTicker(sleepCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.fireSleepTimers(time.Now().Unix())
		}
	}
}

// fireSleepTimers stops playback for every deadline at or before now and
// clears those rows. A dead or disconnected terminal still loses its timer.
func (s *Server) fireSleepTimers(now int64) {
	s.sleepMu.Lock()
	due := []string{}
	for id, stopAt := range s.sleepStop {
		if stopAt > 0 && stopAt <= now {
			due = append(due, id)
			delete(s.sleepStop, id)
		}
	}
	s.sleepMu.Unlock()
	for _, id := range due {
		if _, err := s.db.Exec(`DELETE FROM node_sleep_timer WHERE node_id=?`, id); err != nil {
			slog.Warn("zat sleep timer delete", "node", id, "err", err)
		}
		if err := s.Stop(id); err != nil && !errors.Is(err, ErrNotConnected) {
			slog.Warn("zat sleep timer stop", "node", id, "err", err)
			continue
		}
		slog.Info("zat sleep timer fired", "node", id)
	}
}
