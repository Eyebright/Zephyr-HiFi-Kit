// Package zatserver is the server half of the Zephyr Audio Transport (ZAT): it
// accepts outbound TCP connections from terminal agents, authenticates them by
// node secret, and pushes decoded PCM from the media library to a chosen
// terminal. It is the Roon-like "core → endpoint" engine (PROTOCOL.md).
//
// Transport is pure Go (stdlib + hifihub/zat); decoding delegates to
// internal/decode (pure-Go WAV, CGO libFLAC on Linux). One connection per node;
// frame writes are serialized so control and audio frames never interleave.
package zatserver

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/decode"
	"hifihub/server/internal/library"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
	"hifihub/zat"
)

// ErrNotConnected is returned when a play/stop targets an offline terminal.
var ErrNotConnected = errors.New("终端未连接 ZAT 服务")

// ErrUnknownDevice is returned when a device is not in the terminal's reported list.
var ErrUnknownDevice = errors.New("终端未上报该 ALSA 输出设备")

// PlayStatus is the live playback state of one terminal, for the console.
type PlayStatus struct {
	NodeID     string `json:"node_id"`
	Connected  bool   `json:"connected"`
	SessionID  string `json:"session_id"`
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	Rate       int    `json:"rate"`
	Channels   int    `json:"channels"`
	State      string `json:"state"`
	PTS        uint64 `json:"pts"`
	BufferMS   int    `json:"buffer_ms"`
	Underruns  int    `json:"underruns"`
	Playing    bool   `json:"playing"`
	ALSADevice string `json:"alsa_device"`
	PrefDevice string `json:"pref_device"`
	Devices    []string `json:"devices"`
	// ConfiguredDevice is the server-side DAC suggestion (node.dac_device).
	ConfiguredDevice string `json:"configured_device"`
}

// Server accepts ZAT endpoint connections and drives push playback.
type Server struct {
	addr  string
	nodes *node.Service
	lib   *library.Service
	db    *sql.DB

	ln net.Listener

	mu    sync.Mutex
	conns map[string]*endpoint

	qmu sync.Mutex // serializes queue read-modify-write cycles
}

// New builds a ZAT server. st is used to record playback sessions.
func New(addr string, nodes *node.Service, lib *library.Service, st *store.Store) *Server {
	return &Server{addr: addr, nodes: nodes, lib: lib, db: st.DB, conns: map[string]*endpoint{}}
}

// Addr returns the bound listen address (valid after Listen).
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.addr
	}
	return s.ln.Addr().String()
}

// Listen binds the TCP listener.
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	return nil
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
	}
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

// ListenAndServe binds and serves until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve(ctx)
}

// Close stops the listener and drops all endpoints.
func (s *Server) Close() error {
	s.mu.Lock()
	for _, e := range s.conns {
		e.conn.Close()
	}
	s.conns = map[string]*endpoint{}
	s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// endpoint is one authenticated terminal connection.
type endpoint struct {
	nodeID string
	conn   net.Conn
	srv    *Server

	wmu     sync.Mutex // serializes frame writes
	ctrlSeq uint32

	mu       sync.Mutex
	setupAck chan zat.SetupAck
	play     *playState
	state    zat.State
	pts      uint64
	bufferMS int
	trackID  string
	title    string
	meta     zat.TrackMeta
	// caps is immutable after HELLO; the rest track the latest STATUS report.
	caps       zat.Caps
	alsaDevice string
	prefDevice string
	underruns  int
}

// playState is one active push-playback stream.
type playState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	sessionID string
	trackID   string
}

func (e *endpoint) writeFrame(f zat.Frame) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	return zat.WriteFrame(e.conn, f)
}

func (e *endpoint) control(t zat.Type, v any) error {
	f, err := zat.ControlFrame(t, atomic.AddUint32(&e.ctrlSeq, 1), v)
	if err != nil {
		return err
	}
	return e.writeFrame(f)
}

// handleConn authenticates one endpoint then runs its read loop.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	f, err := zat.ReadFrame(conn)
	if err != nil {
		return
	}
	if f.Type != zat.TypeHello {
		return
	}
	var hello zat.Hello
	if err := json.Unmarshal(f.Payload, &hello); err != nil {
		return
	}
	n, err := s.nodes.Get(hello.NodeID)
	if err != nil {
		s.sendError(conn, zat.ErrAuthFailed, "unknown node")
		return
	}
	if !n.Authorized {
		s.sendError(conn, zat.ErrNotAuthorized, "node not authorized")
		return
	}
	if secretHash(n.Secret) != hello.SecretSHA256 {
		s.sendError(conn, zat.ErrAuthFailed, "secret mismatch")
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}

	e := &endpoint{nodeID: hello.NodeID, conn: conn, srv: s, state: zat.StateAuthenticated, caps: hello.Caps}
	s.mu.Lock()
	if old, ok := s.conns[hello.NodeID]; ok {
		old.conn.Close()
	}
	s.conns[hello.NodeID] = e
	s.mu.Unlock()
	defer s.unregister(e)

	ack := zat.HelloAck{
		SessionID:     "",
		ServerVersion: "zat/0.1",
		Caps: zat.Caps{
			Codecs:      []string{"flac", "pcm_s16le", "pcm_s24le", "pcm_s32le"},
			Formats:     []zat.Format{zat.S16LE, zat.S24LE, zat.S32LE},
			Rates:       []int{44100, 48000, 88200, 96000, 176400, 192000},
			MaxChannels: 2,
		},
		PrebufferMS: 200,
	}
	if err := e.control(zat.TypeHelloAck, ack); err != nil {
		return
	}
	slog.Info("zat endpoint connected", "node", hello.NodeID, "remote", conn.RemoteAddr())
	e.readLoop(ctx)
}

func (s *Server) sendError(conn net.Conn, code, detail string) {
	f, _ := zat.ControlFrame(zat.TypeError, 0, zat.Error{Code: code, Detail: detail, Fatal: true})
	_ = zat.WriteFrame(conn, f)
}

func secretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (s *Server) unregister(e *endpoint) {
	s.mu.Lock()
	if cur, ok := s.conns[e.nodeID]; ok && cur == e {
		delete(s.conns, e.nodeID)
	}
	s.mu.Unlock()
	e.mu.Lock()
	if e.play != nil {
		e.play.cancel()
	}
	e.mu.Unlock()
	_ = s.nodes.SetPlaying(e.nodeID, false, "", 0)
	slog.Info("zat endpoint disconnected", "node", e.nodeID)
}

// readLoop consumes endpoint→server frames until the connection closes.
func (e *endpoint) readLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		f, err := zat.ReadFrame(e.conn)
		if err != nil {
			return
		}
		switch f.Type {
		case zat.TypeSetupAck:
			var ack zat.SetupAck
			if json.Unmarshal(f.Payload, &ack) == nil {
				e.mu.Lock()
				ch := e.setupAck
				e.mu.Unlock()
				if ch != nil {
					select {
					case ch <- ack:
					default:
					}
				}
			}
		case zat.TypeStatus:
			var st zat.Status
			if json.Unmarshal(f.Payload, &st) == nil {
				e.mu.Lock()
				e.pts = st.PTS
				e.bufferMS = st.BufferMS
				e.alsaDevice = st.ALSADevice
				e.prefDevice = st.PrefDevice
				e.underruns = st.Underruns
				// Only a live session adopts the terminal's reported state;
				// stale reports must not resurrect a finished session.
				if e.play != nil {
					e.state = zat.State(st.State)
				}
				e.mu.Unlock()
			}
		case zat.TypePing:
			var p zat.Ping
			_ = json.Unmarshal(f.Payload, &p)
			_ = e.control(zat.TypePong, zat.Pong{Nonce: p.Nonce})
		case zat.TypeTeardown:
			return
		case zat.TypeError:
			var ze zat.Error
			_ = json.Unmarshal(f.Payload, &ze)
			slog.Warn("zat endpoint error", "node", e.nodeID, "code", ze.Code, "detail", ze.Detail)
			if ze.Fatal {
				return
			}
		}
	}
}

// StartPlay pushes a library track to a connected terminal. It returns the
// session id. The stream runs in the background until Stop or EOF.
func (s *Server) StartPlay(ctx context.Context, nodeID, trackID string) (string, error) {
	e := s.endpoint(nodeID)
	if e == nil {
		return "", ErrNotConnected
	}
	track, err := s.lib.GetTrack(trackID)
	if err != nil {
		return "", err
	}
	if !track.Decodable {
		return "", fmt.Errorf("该格式暂不支持解码播放：%s", track.Codec)
	}
	dec, err := decode.Open(track.Path)
	if err != nil {
		return "", err
	}
	n, err := s.nodes.Get(nodeID)
	if err != nil {
		dec.Close()
		return "", err
	}

	// Cancel any prior stream on this endpoint and wait for its teardown so
	// STOP/SETUP frames and the SETUP_ACK channel never interleave between
	// concurrent plays (user action vs. queue auto-advance).
	e.mu.Lock()
	old := e.play
	if old != nil {
		old.cancel()
	}
	e.mu.Unlock()
	if old != nil {
		select {
		case <-old.done:
		case <-time.After(3 * time.Second):
		}
	}

	sessionID := "zat-" + uuid.NewString()
	playCtx, cancel := context.WithCancel(context.Background())
	ps := &playState{ctx: playCtx, cancel: cancel, done: make(chan struct{}), sessionID: sessionID, trackID: trackID}
	ackCh := make(chan zat.SetupAck, 1)
	meta := zat.TrackMeta{
		TrackID: trackID, Title: track.Title, Artist: track.Artist, Album: track.Album,
		DurationMS: track.DurationMS, Codec: track.Codec, Rate: track.Rate, Channels: track.Channels,
	}
	e.mu.Lock()
	e.play = ps
	e.setupAck = ackCh
	e.trackID = trackID
	e.title = track.Title
	e.meta = meta
	e.state = zat.StateReady
	e.mu.Unlock()

	format := dec.Format()
	device := n.DacDevice
	if device == "" {
		device = "default"
	}
	setup := zat.Setup{
		SessionID: sessionID, TrackID: trackID, Format: format.Format, Rate: format.Rate,
		Channels: format.Channels, PrebufferMS: 200, ALSADevice: device,
		TotalSamples: track.TotalSamples,
	}
	// abort tears down a play that never reached the stream goroutine; it must
	// close done so a concurrent StartPlay waiting on us can proceed.
	abort := func(err error) (string, error) {
		cancel()
		dec.Close()
		e.clearPlay(ps)
		close(ps.done)
		return "", err
	}
	if err := e.control(zat.TypeSetup, setup); err != nil {
		return abort(err)
	}
	select {
	case ack := <-ackCh:
		if !ack.OK {
			return abort(fmt.Errorf("终端拒绝播放：%s", ack.Detail))
		}
	case <-ps.ctx.Done():
		return abort(errors.New("播放被新请求取代"))
	case <-time.After(8 * time.Second):
		return abort(errors.New("终端 SETUP 超时"))
	case <-ctx.Done():
		return abort(ctx.Err())
	}

	if err := e.control(zat.TypePlay, zat.Play{SessionID: sessionID}); err != nil {
		return abort(err)
	}
	_ = e.control(zat.TypeTrackMeta, meta)

	now := time.Now().Unix()
	_, _ = s.db.Exec(`INSERT INTO sessions
		(id,node_id,proto,source_client,state,format,volume,dac_device,started_at,ended_at)
		VALUES(?,?,?,?,?,?,?,?,?,0)`,
		sessionID, nodeID, "zat", "library", string(zat.StatePlaying), string(format.Format), 100, device, now)
	if err := s.nodes.SetPlaying(nodeID, true, "zat", 0); err != nil {
		slog.Warn("zat set playing", "node", nodeID, "err", err)
	}
	e.mu.Lock()
	e.state = zat.StatePlaying
	e.pts = 0
	e.mu.Unlock()

	go s.stream(e, ps, dec, track)
	return sessionID, nil
}

// stream pumps PCM to the endpoint until EOF, error or cancellation, then tears
// down the session. It owns the STOP frame and the playing=false transition.
func (s *Server) stream(e *endpoint, ps *playState, dec decode.Decoder, track library.Track) {
	defer dec.Close()
	sink := func(f zat.Frame) error {
		if ps.ctx.Err() != nil {
			return ps.ctx.Err()
		}
		return e.writeFrame(f)
	}
	_, err := decode.StreamFrames(sink, dec, track.ID, 0)
	reason := "eof"
	if ps.ctx.Err() != nil {
		reason = "stopped"
	} else if err != nil {
		reason = "error"
		slog.Warn("zat stream", "node", e.nodeID, "err", err)
	}
	// Sending all PCM is not the end of playback: the terminal still drains
	// its buffer at the ALSA clock. Keep the session live until the terminal
	// reports completion so the console reflects real playback, not the
	// instant the bytes were written.
	if reason == "eof" {
		reason = s.awaitPlaybackEnd(e, ps, track)
	}
	slog.Info("zat stream ended", "node", e.nodeID, "reason", reason)
	_ = e.control(zat.TypeStop, zat.Stop{SessionID: ps.sessionID})
	_ = s.nodes.SetPlaying(e.nodeID, false, "zat", 0)
	_, _ = s.db.Exec(`UPDATE sessions SET state=?,ended_at=? WHERE id=?`,
		string(zat.StateStopped), time.Now().Unix(), ps.sessionID)
	e.mu.Lock()
	e.state = zat.StateStopped
	if e.play == ps {
		e.play = nil
	}
	e.mu.Unlock()
	close(ps.done)
	if reason == "eof" {
		go s.autoAdvance(e.nodeID, ps.trackID)
	}
}

// awaitPlaybackEnd blocks after the source finished sending until the terminal
// finishes playing (STATUS PTS reaches total_samples or it leaves the playing
// state), the user stops, or a grace deadline passes. It returns the teardown
// reason.
func (s *Server) awaitPlaybackEnd(e *endpoint, ps *playState, track library.Track) string {
	deadline := time.Now().Add(time.Duration(track.DurationMS)*time.Millisecond + 5*time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ps.ctx.Done():
			return "stopped"
		case <-tick.C:
			e.mu.Lock()
			pts, state := e.pts, e.state
			e.mu.Unlock()
			if state == zat.StateStopped || state == zat.StateIdle {
				return "eof"
			}
			if track.TotalSamples > 0 && pts >= track.TotalSamples {
				return "eof"
			}
			if time.Now().After(deadline) {
				return "eof"
			}
		}
	}
}

func (e *endpoint) clearPlay(ps *playState) {
	e.mu.Lock()
	if e.play == ps {
		e.play = nil
		e.setupAck = nil
	}
	e.mu.Unlock()
}

// Stop cancels the active stream and waits for teardown. It is idempotent.
func (s *Server) Stop(nodeID string) error {
	e := s.endpoint(nodeID)
	if e == nil {
		return ErrNotConnected
	}
	e.mu.Lock()
	ps := e.play
	e.mu.Unlock()
	if ps == nil {
		_ = e.control(zat.TypeStop, zat.Stop{})
		_ = s.nodes.SetPlaying(nodeID, false, "zat", 0)
		return nil
	}
	ps.cancel()
	select {
	case <-ps.done:
	case <-time.After(8 * time.Second):
		return errors.New("停止播放超时")
	}
	return nil
}

// Status reports the live playback state of a terminal.
func (s *Server) Status(nodeID string) (PlayStatus, error) {
	st := PlayStatus{NodeID: nodeID, State: string(zat.StateIdle), Devices: []string{}}
	if n, err := s.nodes.Get(nodeID); err == nil {
		st.ConfiguredDevice = n.DacDevice
	}
	e := s.endpoint(nodeID)
	if e == nil {
		return st, nil
	}
	e.mu.Lock()
	st.Connected = true
	st.State = string(e.state)
	st.PTS = e.pts
	st.BufferMS = e.bufferMS
	st.Underruns = e.underruns
	st.TrackID = e.trackID
	st.Title = e.title
	st.Artist = e.meta.Artist
	st.Album = e.meta.Album
	st.DurationMS = e.meta.DurationMS
	st.Codec = e.meta.Codec
	st.Rate = e.meta.Rate
	st.Channels = e.meta.Channels
	st.ALSADevice = e.alsaDevice
	st.PrefDevice = e.prefDevice
	if e.play != nil {
		st.SessionID = e.play.sessionID
		st.Playing = e.state == zat.StatePlaying
	}
	e.mu.Unlock()
	if len(e.caps.ALSADevices) > 0 {
		st.Devices = e.caps.ALSADevices
	}
	return st, nil
}

// SetDevice selects the terminal's preferred ALSA output device. The choice is
// persisted as the node's DAC device (the SETUP suggestion) and pushed over ZAT;
// the terminal applies it at the next SETUP, like its local device picker.
func (s *Server) SetDevice(nodeID, device string) error {
	e := s.endpoint(nodeID)
	if e == nil {
		return ErrNotConnected
	}
	if device == "" {
		return ErrUnknownDevice
	}
	if device != "default" {
		known := false
		for _, d := range e.caps.ALSADevices {
			if d == device {
				known = true
				break
			}
		}
		if !known {
			return ErrUnknownDevice
		}
	}
	if _, err := s.nodes.Patch(nodeID, node.Patch{DacDevice: &device}); err != nil {
		return err
	}
	return e.control(zat.TypeSetDevice, zat.SetDevice{Device: device})
}

// Connected reports whether a terminal currently holds a ZAT connection.
func (s *Server) Connected(nodeID string) bool { return s.endpoint(nodeID) != nil }

func (s *Server) endpoint(nodeID string) *endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns[nodeID]
}
