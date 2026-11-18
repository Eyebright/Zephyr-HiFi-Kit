package zatclient

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"hifihub/agent/internal/player"
	"hifihub/zat"
)

const (
	pcmQueueChunks = 8 // bounded PCM queue: full queue backpressures the source
	statusEvery    = 500 * time.Millisecond
)

type audioChunk struct {
	pcm     []byte
	pts     uint64
	samples int
}

type renderCmdKind int

const (
	cmdSetup renderCmdKind = iota
	cmdStop
)

type renderCmd struct {
	kind   renderCmdKind
	setup  zat.Setup
	device string
	ack    chan error
}

// session holds one connection's playback state. The render goroutine owns the
// player; readLoop owns frame reads and the shared scalars below (guarded by mu).
type session struct {
	c      *Client
	conn   net.Conn
	nodeID string

	wmu sync.Mutex // serializes frame writes
	seq uint32

	mu          sync.Mutex
	state       zat.State
	sessionID   string
	meta        zat.TrackMeta
	setup       zat.Setup
	device      string
	pts         uint64
	volume      int
	paused      bool
	resumeCh    chan struct{} // closed on resume; replaced on each pause
	skipUntil   uint64
	queuedBytes int64
	bytesPerMS  int
	frameBytes  int
	prebufferMS int

	pcmCh      chan audioChunk
	ctrlCh     chan renderCmd
	stopRender chan struct{}
	renderDone chan struct{}
	closeOnce  sync.Once
}

func newSession(c *Client, conn net.Conn, nodeID string) *session {
	return &session{
		c: c, conn: conn, nodeID: nodeID,
		state: zat.StateAuthenticated, volume: 100,
		pcmCh: make(chan audioChunk, pcmQueueChunks), ctrlCh: make(chan renderCmd, 4),
		stopRender: make(chan struct{}), renderDone: make(chan struct{}),
	}
}

func (s *session) start() { go s.renderLoop() }

func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.stopRender)
		select {
		case <-s.renderDone:
		case <-time.After(2 * time.Second):
		}
		_ = s.conn.Close()
	})
}

// write sends one control frame, serialized against the STATUS loop.
func (s *session) write(t zat.Type, v any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.seq++
	f, err := zat.ControlFrame(t, s.seq, v)
	if err != nil {
		return err
	}
	return zat.WriteFrame(s.conn, f)
}

func (s *session) addQueued(n int64) {
	s.mu.Lock()
	s.queuedBytes += n
	if s.queuedBytes < 0 {
		s.queuedBytes = 0
	}
	s.mu.Unlock()
}

func (s *session) bufferMSLocked() int {
	if s.bytesPerMS <= 0 {
		return 0
	}
	return int(s.queuedBytes / int64(s.bytesPerMS))
}

func (s *session) snapshotLocked() Snapshot {
	trackID := s.meta.TrackID
	if trackID == "" {
		trackID = s.setup.TrackID
	}
	return Snapshot{
		Connected: true, State: string(s.state), SessionID: s.sessionID,
		TrackID: trackID, Title: s.meta.Title, Artist: s.meta.Artist, Album: s.meta.Album,
		DurationMS: s.meta.DurationMS, Codec: s.meta.Codec, Rate: s.setup.Rate, Channels: s.setup.Channels,
		PTS: s.pts, BufferMS: s.bufferMSLocked(), Volume: s.volume, ALSADevice: s.device,
		PrefDevice: s.c.PreferredDevice(),
	}
}

func (s *session) publish() {
	s.mu.Lock()
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.c.setSnap(func(sn *Snapshot) { *sn = snap })
}

// renderLoop owns the ALSA player: it opens on SETUP, writes PCM (blocking at the
// sample clock, which backpressures the source), and drains/closes on STOP.
// While paused the PCM channel is disabled (nil), holding queued chunks instead
// of rendering them, so resume continues seamlessly; control frames still flow.
func (s *session) renderLoop() {
	defer close(s.renderDone)
	var p player.Player
	for {
		s.mu.Lock()
		paused := s.paused
		var resumeCh chan struct{}
		if paused {
			resumeCh = s.resumeCh
		}
		s.mu.Unlock()
		var pcmCh chan audioChunk
		if !paused {
			pcmCh = s.pcmCh
		}
		select {
		case <-s.stopRender:
			if p != nil {
				_ = p.Drain()
				_ = p.Close()
			}
			return
		case <-resumeCh:
			// Resumed: re-loop so the PCM channel is re-enabled.
		case cmd := <-s.ctrlCh:
			switch cmd.kind {
			case cmdSetup:
				if p != nil {
					// Drop, not Drain: a re-SETUP (seek or track change) must cut
					// the old buffer immediately instead of playing out its tail.
					_ = p.Drop()
					_ = p.Close()
					p = nil
				}
				// Discard stale chunks from the previous session; the source
				// only sends new audio after the SETUP_ACK, so nothing new is
				// lost here.
				for {
					select {
					case <-s.pcmCh:
						continue
					default:
					}
					break
				}
				np := s.c.newPlayer()
				err := np.Open(cmd.setup.Format, cmd.setup.Rate, cmd.setup.Channels, cmd.device)
				if err != nil {
					cmd.ack <- err
					continue
				}
				p = np
				cmd.ack <- nil
			case cmdStop:
				if p != nil {
					_ = p.Drain()
					_ = p.Close()
					p = nil
				}
				s.mu.Lock()
				s.queuedBytes = 0
				s.mu.Unlock()
				cmd.ack <- nil
			}
		case chunk := <-pcmCh:
			if p == nil {
				s.addQueued(-int64(len(chunk.pcm)))
				continue
			}
			s.mu.Lock()
			format, vol := s.setup.Format, s.volume
			s.mu.Unlock()
			pcm := applyVolume(chunk.pcm, format, vol)
			err := p.Write(pcm)
			s.addQueued(-int64(len(chunk.pcm)))
			if err != nil {
				_ = s.write(zat.TypeError, zat.Error{SessionID: s.sessionID, Code: zat.ErrUnderrun, Detail: err.Error(), Fatal: false})
				continue
			}
			s.mu.Lock()
			s.pts = chunk.pts + uint64(chunk.samples)
			s.mu.Unlock()
			s.publish()
		}
	}
}

// readLoop consumes source frames until the connection closes or ctx is cancelled.
func (s *session) readLoop(ctx context.Context, prebufferMS int) error {
	s.mu.Lock()
	s.prebufferMS = prebufferMS
	s.mu.Unlock()
	go s.statusLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = s.conn.Close()
	}()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fr, err := zat.ReadFrame(s.conn)
		if err != nil {
			return err
		}
		switch fr.Type {
		case zat.TypeSetup:
			if err := s.handleSetup(ctx, fr); err != nil {
				return err
			}
		case zat.TypePlay:
			var pl zat.Play
			_ = json.Unmarshal(fr.Payload, &pl)
			s.mu.Lock()
			s.state = zat.StatePlaying
			if s.paused {
				close(s.resumeCh)
			}
			s.paused = false
			if pl.StartPTS > 0 {
				s.skipUntil = pl.StartPTS
				s.pts = pl.StartPTS
			}
			s.mu.Unlock()
			s.publish()
		case zat.TypePause:
			s.mu.Lock()
			s.paused = true
			s.resumeCh = make(chan struct{})
			s.state = zat.StatePaused
			s.mu.Unlock()
			s.publish()
		case zat.TypeSeek:
			var sk zat.Seek
			_ = json.Unmarshal(fr.Payload, &sk)
			s.seek(sk.PTS)
		case zat.TypeStop:
			s.stopPlayback()
		case zat.TypeVolume:
			var v zat.Volume
			_ = json.Unmarshal(fr.Payload, &v)
			s.setVolume(v.Volume)
		case zat.TypeSetDevice:
			var d zat.SetDevice
			_ = json.Unmarshal(fr.Payload, &d)
			s.c.SetDevice(d.Device)
		case zat.TypeTrackMeta:
			var m zat.TrackMeta
			if json.Unmarshal(fr.Payload, &m) == nil {
				s.mu.Lock()
				s.meta = m
				s.mu.Unlock()
				s.publish()
			}
		case zat.TypeAudio:
			s.handleAudio(ctx, fr)
		case zat.TypePing:
			var p zat.Ping
			_ = json.Unmarshal(fr.Payload, &p)
			_ = s.write(zat.TypePong, zat.Pong{Nonce: p.Nonce})
		case zat.TypeTeardown:
			s.stopPlayback()
			return nil
		case zat.TypeError:
			var ze zat.Error
			_ = json.Unmarshal(fr.Payload, &ze)
			if ze.Fatal {
				return errors.New("zat source error: " + ze.Code)
			}
		}
	}
}

func (s *session) handleSetup(ctx context.Context, fr zat.Frame) error {
	var su zat.Setup
	if err := json.Unmarshal(fr.Payload, &su); err != nil {
		return nil
	}
	device := su.ALSADevice
	if pref := s.c.PreferredDevice(); pref != "" {
		device = pref
	}
	if device == "" {
		device = "default"
	}
	bpf := su.Format.BytesPerSample() * su.Channels
	s.mu.Lock()
	s.sessionID = su.SessionID
	s.setup = su
	s.device = device
	s.pts = 0
	s.skipUntil = 0
	s.paused = false
	s.queuedBytes = 0
	s.frameBytes = bpf
	s.bytesPerMS = su.Rate * bpf / 1000
	s.state = zat.StateReady
	s.mu.Unlock()

	ackCh := make(chan error, 1)
	select {
	case s.ctrlCh <- renderCmd{kind: cmdSetup, setup: su, device: device, ack: ackCh}:
	case <-ctx.Done():
		return ctx.Err()
	}
	var openErr error
	select {
	case openErr = <-ackCh:
	case <-time.After(5 * time.Second):
		openErr = errors.New("ALSA 设备打开超时")
	case <-ctx.Done():
		return ctx.Err()
	}
	ack := zat.SetupAck{
		SessionID: su.SessionID, OK: openErr == nil, BufferFrames: 4096,
		Opened: zat.OpenedParams{Format: su.Format, Rate: su.Rate, Channels: su.Channels},
	}
	if openErr != nil {
		ack.Detail = openErr.Error()
	}
	_ = s.write(zat.TypeSetupAck, ack)
	s.publish()
	return nil
}

func (s *session) handleAudio(ctx context.Context, fr zat.Frame) {
	ap, err := zat.DecodeAudio(fr.Payload)
	if err != nil {
		return
	}
	s.mu.Lock()
	fb := s.frameBytes
	paused := s.paused
	skip := s.skipUntil
	playing := s.state == zat.StatePlaying
	s.mu.Unlock()
	if fb <= 0 {
		return
	}
	samples := len(ap.PCM) / fb
	endPTS := ap.PTS + uint64(samples)
	// Discard while paused, before PLAY, or before a forward-seek target. Keeping
	// the read loop draining (rather than blocking) lets control frames through
	// and avoids a pause deadlock; the bounded queue below paces the source.
	if paused || !playing || endPTS <= skip {
		return
	}
	select {
	case s.pcmCh <- audioChunk{pcm: ap.PCM, pts: ap.PTS, samples: samples}:
		s.addQueued(int64(len(ap.PCM)))
	case <-ctx.Done():
	}
}

// stopPlayback drains and closes ALSA and marks the session stopped.
func (s *session) stopPlayback() {
	s.mu.Lock()
	s.state = zat.StateStopped
	s.paused = false
	s.mu.Unlock()
	ackCh := make(chan error, 1)
	select {
	case s.ctrlCh <- renderCmd{kind: cmdStop, ack: ackCh}:
		<-ackCh
	case <-time.After(2 * time.Second):
	}
	s.publish()
}

func (s *session) statusLoop(ctx context.Context) {
	t := time.NewTicker(statusEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			sid := s.sessionID
			st := zat.Status{
				SessionID: sid, State: string(s.state), PTS: s.pts,
				BufferMS: s.bufferMSLocked(), ALSADevice: s.device,
				PrefDevice: s.c.PreferredDevice(),
			}
			s.mu.Unlock()
			// Report even before the first SETUP so the source can monitor the
			// live connection and the preferred device while idle.
			if err := s.write(zat.TypeStatus, st); err != nil {
				return
			}
		}
	}
}

// --- local controls ---

func (s *session) togglePause() bool {
	s.mu.Lock()
	if s.state == zat.StatePlaying {
		s.paused = true
		s.resumeCh = make(chan struct{})
		s.state = zat.StatePaused
	} else if s.state == zat.StatePaused {
		close(s.resumeCh)
		s.paused = false
		s.state = zat.StatePlaying
	}
	paused := s.paused
	s.mu.Unlock()
	s.publish()
	return paused
}

func (s *session) localStop() {
	s.mu.Lock()
	sid := s.sessionID
	s.mu.Unlock()
	_ = s.write(zat.TypeTeardown, zat.Teardown{SessionID: sid, Reason: "user_stop"})
	s.stopPlayback()
}

func (s *session) seek(pts uint64) {
	s.mu.Lock()
	s.skipUntil = pts
	s.mu.Unlock()
	s.publish()
}

func (s *session) setVolume(v int) {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	s.mu.Lock()
	s.volume = v
	s.mu.Unlock()
	s.publish()
}

// applyVolume scales interleaved little-endian PCM by volume/100 (soft volume,
// PROTOCOL.md §8). Unity (100) returns the input unchanged; 0 returns silence.
func applyVolume(pcm []byte, format zat.Format, volume int) []byte {
	if volume >= 100 {
		return pcm
	}
	bps := format.BytesPerSample()
	if bps == 0 {
		return pcm
	}
	out := make([]byte, len(pcm))
	if volume <= 0 {
		return out
	}
	for i := 0; i+bps <= len(pcm); i += bps {
		var v int32
		switch bps {
		case 2:
			v = int32(int16(binary.LittleEndian.Uint16(pcm[i:])))
		case 3:
			v = int32(pcm[i]) | int32(pcm[i+1])<<8 | int32(pcm[i+2])<<16
			if v&0x800000 != 0 {
				v |= ^0xFFFFFF
			}
		case 4:
			v = int32(binary.LittleEndian.Uint32(pcm[i:]))
		}
		v = int32(int64(v) * int64(volume) / 100)
		switch bps {
		case 2:
			binary.LittleEndian.PutUint16(out[i:], uint16(int16(v)))
		case 3:
			out[i], out[i+1], out[i+2] = byte(v), byte(v>>8), byte(v>>16)
		case 4:
			binary.LittleEndian.PutUint32(out[i:], uint32(v))
		}
	}
	return out
}
