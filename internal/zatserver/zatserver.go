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
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/decode"
	"hifihub/server/internal/library"
	"hifihub/server/internal/mqa"
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
	NodeID     string           `json:"node_id"`
	Connected  bool             `json:"connected"`
	SessionID  string           `json:"session_id"`
	TrackID    string           `json:"track_id"`
	Title      string           `json:"title"`
	Artist     string           `json:"artist"`
	Album      string           `json:"album"`
	DurationMS int              `json:"duration_ms"`
	Codec      string           `json:"codec"`
	Rate       int              `json:"rate"`
	Channels   int              `json:"channels"`
	State      string           `json:"state"`
	PTS        uint64           `json:"pts"`
	BufferMS   int              `json:"buffer_ms"`
	Underruns  int              `json:"underruns"`
	Playing    bool             `json:"playing"`
	Volume     int              `json:"volume"`
	ALSADevice string           `json:"alsa_device"`
	PrefDevice string           `json:"pref_device"`
	// SleepStopAt is the unix deadline of the node's sleep timer, 0 when off.
	SleepStopAt int64            `json:"sleep_stop_at"`
	Devices    []string         `json:"devices"`
	DeviceInfo []zat.ALSADevice `json:"device_info,omitempty"`
	// ConfiguredDevice is the server-side DAC suggestion (node.dac_device).
	ConfiguredDevice string `json:"configured_device"`
	MQA              bool   `json:"mqa"`
	BitPerfect       bool   `json:"bit_perfect"`
	MQACapable       bool   `json:"mqa_capable"`
	DoP              bool   `json:"dop"`
	DSDRate          int    `json:"dsd_rate"`
	DoPCapable       bool   `json:"dop_capable"`
	Converted        bool   `json:"converted"`
}

var (
	// ErrMQACapability is returned when the terminal lacks strict MQA support.
	ErrMQACapability = errors.New("终端版本不支持 MQA 位完美输出，请先升级终端")
	// ErrMQADevice is returned when the chosen output device converts audio.
	ErrMQADevice = errors.New("MQA 位完美输出要求直连 hw: 设备")
	// ErrMQALocked is returned when volume changes would alter MQA bits.
	ErrMQALocked = errors.New("MQA 位完美播放已锁定软件音量")
	// ErrDoPCapability is returned when the terminal cannot carry DoP.
	ErrDoPCapability = errors.New("终端版本不支持 DSD DoP 直通，请先升级终端")
	// ErrDoPDevice is returned when the output device would convert DoP PCM.
	ErrDoPDevice = errors.New("DSD 位完美输出要求直连 hw: 设备")
	// ErrDoPLocked is returned when volume changes would alter DoP bits.
	ErrDoPLocked = errors.New("DSD 位完美播放已锁定软件音量")
)

// Server accepts ZAT endpoint connections and drives push playback.
type Server struct {
	addr  string
	nodes *node.Service
	lib   *library.Service
	db    *sql.DB

	ln net.Listener

	mu    sync.Mutex
	conns map[string]*endpoint

	libraryGate sync.RWMutex
	qmu         sync.Mutex // serializes queue read-modify-write cycles
	detectSem   chan struct{}
	sleepMu     sync.Mutex
	sleepStop   map[string]int64
}

// New builds a ZAT server. st is used to record playback sessions.
func New(addr string, nodes *node.Service, lib *library.Service, st *store.Store) *Server {
	s := &Server{addr: addr, nodes: nodes, lib: lib, db: st.DB, conns: map[string]*endpoint{}, detectSem: make(chan struct{}, 2),
		sleepStop: map[string]int64{}}
	s.loadSleepTimers()
	return s
}

func (s *Server) WithLibraryMaintenance(fn func()) {
	s.libraryGate.Lock()
	defer s.libraryGate.Unlock()
	fn()
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
	go s.runSleepTimers(ctx)
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
	// Pacing state: keeps in-flight audio near paceMaxMS so control frames
	// (PAUSE/SEEK/STOP) never queue behind seconds of buffered PCM.
	bytesPerMS int
	lastStatus time.Time
	sentSince  int64
	trackID    string
	title      string
	meta       zat.TrackMeta
	// caps is set at HELLO; STATUS may refresh only the ALSA device lists.
	caps       zat.Caps
	alsaDevice string
	prefDevice string
	underruns  int
	volume     int // last server-set soft volume; STATUS does not carry it
	mqa        bool
	dop        bool
	dsdRate    int
	converted  bool
	bitPerfect bool
}

// playState is one active push-playback stream.
type playState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	sessionID string
	trackID   string
	listening listeningClock

	format       zat.Format
	device       string
	totalSamples uint64
	rate         int
	mqa          bool
	dop          bool
	dsdRate      int
	converted    bool
	bitPerfect   bool

	mu         sync.Mutex
	paused     bool
	resumeCh   chan struct{} // closed on resume; replaced on each pause
	seeking    bool
	seekTo     uint64
	seekWake   chan struct{} // closed on seek; wakes awaitPlaybackEnd
	pumpCancel context.CancelFunc
}

func (ps *playState) setPump(cancel context.CancelFunc) {
	ps.mu.Lock()
	ps.pumpCancel = cancel
	ps.mu.Unlock()
}

// waitWhilePaused gates the PCM pump while paused. The terminal discards audio
// sent during pause, so the server must stop sending too or resume loses data.
func (ps *playState) waitWhilePaused(ctx context.Context) error {
	ps.mu.Lock()
	for ps.paused {
		ch := ps.resumeCh
		ps.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
		ps.mu.Lock()
	}
	ps.mu.Unlock()
	return nil
}

func (ps *playState) isPaused() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.paused
}

// requestSeek records a seek target and interrupts the current pump (or the
// end-of-stream drain wait). The stream loop restarts decoding from the target.
func (ps *playState) requestSeek(target uint64) {
	ps.mu.Lock()
	ps.seeking = true
	ps.seekTo = target
	close(ps.seekWake)
	ps.seekWake = make(chan struct{})
	cancel := ps.pumpCancel
	ps.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (ps *playState) takeSeek() (uint64, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !ps.seeking {
		return 0, false
	}
	ps.seeking = false
	return ps.seekTo, true
}

func (ps *playState) seekPending() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.seeking
}

// paceMaxMS caps the estimated terminal-side audio buffer. The terminal reads
// frames sequentially and blocks when its PCM queue is full, so a deep buffer
// delays PAUSE/SEEK by seconds; a shallow one keeps controls responsive while
// tolerating LAN jitter.
const paceMaxMS = 600

func (e *endpoint) resetPacing(bytesPerMS int) {
	e.mu.Lock()
	e.bytesPerMS = bytesPerMS
	e.bufferMS = 0
	e.sentSince = 0
	e.lastStatus = time.Now()
	e.mu.Unlock()
}

// bufferedEstimateMS estimates terminal-side buffered audio: the last reported
// queue plus what was sent since, minus what the sample clock consumed meanwhile.
func (e *endpoint) bufferedEstimateMS() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bytesPerMS <= 0 {
		return 0
	}
	est := int64(e.bufferMS) + e.sentSince/int64(e.bytesPerMS) - time.Since(e.lastStatus).Milliseconds()
	if est < 0 {
		est = 0
	}
	return int(est)
}

func (e *endpoint) noteAudioSent(n int) {
	e.mu.Lock()
	e.sentSince += int64(n)
	e.mu.Unlock()
}

// errSeekInterrupt unwinds the pump when a seek lands mid-stream (including
// during a reSetup wait, when the previous pump cancel is already spent).
var errSeekInterrupt = errors.New("seek interrupt")

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
	if subtle.ConstantTimeCompare([]byte(secretHash(n.Secret)), []byte(hello.SecretSHA256)) != 1 {
		s.sendError(conn, zat.ErrAuthFailed, "secret mismatch")
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}

	e := &endpoint{nodeID: hello.NodeID, conn: conn, srv: s, state: zat.StateAuthenticated, caps: hello.Caps, volume: 100}
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
				e.acceptStatus(st, time.Now())
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

// detectMQABounded scans a file for MQA with a hard deadline. A stuck network
// read must not block playback forever, so the scan runs detached: the play
// path waits at most mqaDetectTimeout, and a result that arrives later is still
// persisted for subsequent plays. The semaphore keeps stalled scans from
// accumulating worker goroutines and open handles.
func (s *Server) detectMQABounded(path, trackID string) (mqa.Result, bool) {
	select {
	case s.detectSem <- struct{}{}:
	default:
		slog.Warn("zat mqa detect busy, skipping", "track", trackID)
		return mqa.Result{}, false
	}
	type outcome struct{ res mqa.Result }
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		defer func() { <-s.detectSem }()
		res, err := mqa.DetectFile(path)
		if err != nil {
			slog.Warn("zat mqa detect", "track", trackID, "err", err)
			return
		}
		if merr := s.lib.MarkMQA(trackID, res.Detected); merr != nil {
			slog.Warn("zat mark mqa", "track", trackID, "err", merr)
		}
		slog.Info("zat mqa detect done", "track", trackID, "detected", res.Detected, "elapsed", time.Since(start).Round(time.Millisecond).String())
		done <- outcome{res: res}
	}()
	select {
	case o := <-done:
		return o.res, true
	case <-time.After(mqaDetectTimeout):
		slog.Warn("zat mqa detect timeout, playing unchecked", "track", trackID, "timeout", mqaDetectTimeout.String())
		return mqa.Result{}, false
	}
}

const mqaDetectTimeout = 20 * time.Second

// StartPlay pushes a library track to a connected terminal. It returns the
// session id. The stream runs in the background until Stop or EOF.
func (s *Server) StartPlay(ctx context.Context, nodeID, trackID string) (string, error) {
	s.libraryGate.RLock()
	defer s.libraryGate.RUnlock()
	e := s.endpoint(nodeID)
	if e == nil {
		return "", ErrNotConnected
	}
	track, err := s.lib.GetTrack(trackID)
	if err != nil {
		return "", err
	}
	if !track.Decodable && track.Codec != "dsd" {
		return "", fmt.Errorf("该格式暂不支持解码播放：%s", track.Codec)
	}
	playbackPath, err := s.lib.PlaybackPath(track.ID)
	if err != nil {
		return "", err
	}
	// Untagged MQA streams hide their sync word in the PCM; detect once here and
	// persist so later plays and the browse UI see the marker without a rescan.
	// Detection reads over the network mount and has been observed to stall
	// indefinitely on a dead SMB handle, so it must never hold the play request
	// hostage: bound it, and let a late result persist in the background.
	if !track.MQAChecked && mqaCandidate(track) {
		if res, ok := s.detectMQABounded(playbackPath, track.ID); ok {
			track.MQAChecked = true
			track.MQA = res.Detected
		}
	}
	slog.Info("zat play prepare", "node", nodeID, "track", trackID, "mqa", track.MQA, "checked", track.MQAChecked)
	dec, err := openTrackFile(playbackPath, track)
	if err != nil {
		return "", err
	}
	slog.Info("zat play decoder open", "node", nodeID, "track", trackID, "format", dec.Format().Format, "rate", dec.Format().Rate)
	dop, dsdRate := false, 0
	if info, ok := dec.(interface {
		DoP() bool
		DSDRate() int
	}); ok && info.DoP() {
		dop, dsdRate = true, info.DSDRate()
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
	ps := &playState{ctx: playCtx, cancel: cancel, done: make(chan struct{}), sessionID: sessionID, trackID: trackID,
		resumeCh: make(chan struct{}), seekWake: make(chan struct{})}
	format := dec.Format()
	if dop {
		total := dec.TotalSamples()
		dur := 0
		if format.Rate > 0 {
			dur = int(total * 1000 / uint64(format.Rate))
		}
		if track.TotalSamples != total || track.DurationMS != dur || !track.Decodable {
			track.TotalSamples, track.DurationMS, track.Decodable = total, dur, true
			if err := s.lib.MarkDSD(track.ID, dsdRate, format.Channels, 1, total, dur); err != nil {
				slog.Warn("zat mark dsd", "track", track.ID, "err", err)
			}
		}
	}
	metaRate := track.Rate
	if dop {
		metaRate = format.Rate
	}
	meta := zat.TrackMeta{
		TrackID: trackID, Title: track.Title, Artist: track.Artist, Album: track.Album,
		DurationMS: track.DurationMS, Codec: track.Codec, Rate: metaRate, Channels: track.Channels,
		MQA: track.MQA, DoP: dop, DSDRate: dsdRate,
	}
	device := n.DacDevice
	if device == "" {
		device = "default"
	}
	// MQA and DoP need an untouched bit path: a capable agent, strict hw output,
	// and no software volume. Refuse early instead of playing degraded audio.
	if track.MQA || dop {
		if track.MQA && !e.caps.MQA {
			dec.Close()
			return "", ErrMQACapability
		}
		if dop && !e.caps.DoP {
			dec.Close()
			return "", ErrDoPCapability
		}
		if !strings.HasPrefix(device, "hw:") {
			dec.Close()
			if dop {
				return "", fmt.Errorf("%w（当前 %s）", ErrDoPDevice, device)
			}
			return "", fmt.Errorf("%w（当前 %s）", ErrMQADevice, device)
		}
	}
	ps.format, ps.device, ps.totalSamples, ps.rate, ps.mqa = format.Format, device, dec.TotalSamples(), format.Rate, track.MQA
	ps.dop, ps.dsdRate = dop, dsdRate
	e.mu.Lock()
	e.play = ps
	e.trackID = trackID
	e.title = track.Title
	e.meta = meta
	e.state = zat.StateReady
	if track.MQA || dop {
		e.volume = 100 // strict sessions force unity gain on the terminal
	}
	e.mu.Unlock()

	e.resetPacing(format.Rate * format.Channels * format.Format.BytesPerSample() / 1000)
	setup := zat.Setup{
		SessionID: sessionID, TrackID: trackID, Format: format.Format, Rate: format.Rate,
		Channels: format.Channels, PrebufferMS: 200, ALSADevice: device,
		TotalSamples: dec.TotalSamples(), MQA: track.MQA, DoP: dop, DSDRate: dsdRate,
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
	slog.Info("zat play setup sent", "node", nodeID, "track", trackID, "device", device, "rate", format.Rate)
	ack, ackErr := s.awaitSetup(e, ps, setup, ctx)
	slog.Info("zat play setup ack", "node", nodeID, "track", trackID, "ok", ack.OK, "bit_perfect", ack.BitPerfect, "err", ackErr)
	if ackErr == nil && ack.OK && (!(track.MQA || dop) || ack.BitPerfect) {
		ps.bitPerfect = ack.BitPerfect
	} else {
		if !dop {
			return abort(setupFailure(ack, ackErr))
		}
		// The terminal could not carry DoP at the carrier rate. Fall back to
		// server-side ffmpeg PCM so the track still plays, marked as converted.
		dec.Close()
		var ferr error
		for _, rate := range []int{176400, 88200} {
			dec, ferr = openFallbackPCM(playbackPath, rate, 2, track.DurationMS)
			if ferr == nil {
				break
			}
		}
		if ferr != nil {
			return abort(fmt.Errorf("DSD DoP 失败（%v），且 ffmpeg PCM 回退不可用：%w", setupFailure(ack, ackErr), ferr))
		}
		converted := true
		dop = false
		format = dec.Format()
		ps.format, ps.rate, ps.totalSamples = format.Format, format.Rate, dec.TotalSamples()
		ps.dop, ps.converted = false, converted
		meta.Rate, meta.DoP, meta.DSDRate = format.Rate, false, dsdRate
		if format.Rate > 0 {
			meta.DurationMS = int(dec.TotalSamples() * 1000 / uint64(format.Rate))
		}
		e.mu.Lock()
		e.meta = meta
		e.mu.Unlock()
		e.resetPacing(format.Rate * format.Channels * format.Format.BytesPerSample() / 1000)
		setup = zat.Setup{
			SessionID: sessionID, TrackID: trackID, Format: format.Format, Rate: format.Rate,
			Channels: format.Channels, PrebufferMS: 200, ALSADevice: device,
			TotalSamples: dec.TotalSamples(), MQA: false, DoP: false, DSDRate: dsdRate,
		}
		ack, ackErr = s.awaitSetup(e, ps, setup, ctx)
		if ackErr != nil || !ack.OK {
			return abort(fmt.Errorf("DSD PCM 回退失败：%w", setupFailure(ack, ackErr)))
		}
		ps.bitPerfect = false
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
	historyID := "h-" + uuid.NewString()
	_, historyErr := s.db.Exec(`INSERT INTO play_history
		(id,node_id,track_id,started_at,title,artist,album,genre,cover_hash,measured) VALUES(?,?,?,?,?,?,?,?,?,1)`,
		historyID, nodeID, trackID, now, track.Title, track.Artist, track.Album, track.Genre, track.CoverHash)
	if historyErr != nil {
		slog.Warn("zat record history", "node", nodeID, "err", historyErr)
	} else {
		ps.listening.start(historyID, format.Rate, time.Now())
	}
	if err := s.nodes.SetPlaying(nodeID, true, "zat", 0); err != nil {
		slog.Warn("zat set playing", "node", nodeID, "err", err)
	}
	e.mu.Lock()
	e.state = zat.StatePlaying
	e.pts = 0
	e.mqa = track.MQA
	e.dop = dop
	e.dsdRate = dsdRate
	e.converted = ps.converted
	e.bitPerfect = ps.bitPerfect
	e.mu.Unlock()

	go s.stream(e, ps, dec, track)
	return sessionID, nil
}

// stream pumps PCM to the endpoint until EOF, error or cancellation, then tears
// down the session. It owns the STOP frame and the playing=false transition.
// Pause gates the pump; a seek cancels the pump and restarts decoding from the
// target sample with a fresh SETUP, without touching history or auto-advance.
func (s *Server) stream(e *endpoint, ps *playState, dec decode.Decoder, track library.Track) {
	startSample := uint64(0)
	reason := "eof"
	for {
		pumpCtx, pumpCancel := context.WithCancel(ps.ctx)
		ps.setPump(pumpCancel)
		sink := func(f zat.Frame) error {
			for {
				if err := ps.waitWhilePaused(pumpCtx); err != nil {
					return err
				}
				if pumpCtx.Err() != nil {
					return pumpCtx.Err()
				}
				if ps.seekPending() {
					return errSeekInterrupt
				}
				if e.bufferedEstimateMS() <= paceMaxMS {
					break
				}
				select {
				case <-pumpCtx.Done():
					return pumpCtx.Err()
				case <-time.After(40 * time.Millisecond):
				}
			}
			if err := e.writeFrame(f); err != nil {
				return err
			}
			e.noteAudioSent(len(f.Payload))
			return nil
		}
		_, err := decode.StreamFramesFrom(sink, dec, track.ID, 0, startSample)
		pumpCancel()
		dec.Close()
		if ps.ctx.Err() != nil {
			reason = "stopped"
			break
		}
		if target, ok := ps.takeSeek(); ok {
			nd, oerr := s.openTrackDecoder(track, ps)
			if oerr != nil {
				reason = "error"
				slog.Warn("zat seek reopen", "node", e.nodeID, "err", oerr)
				break
			}
			dec = nd
			startSample = target
			if serr := s.reSetup(e, ps, track, target); serr != nil {
				reason = "error"
				slog.Warn("zat seek setup", "node", e.nodeID, "err", serr)
				break
			}
			continue
		}
		if err != nil {
			reason = "error"
			slog.Warn("zat stream", "node", e.nodeID, "err", err)
			break
		}
		// Sending all PCM is not the end of playback: the terminal still drains
		// its buffer at the ALSA clock. Keep the session live until the terminal
		// reports completion, a seek restarts it, or the user stops.
		ar := s.awaitPlaybackEnd(e, ps, track)
		if ar == "seek" {
			target, _ := ps.takeSeek()
			nd, oerr := s.openTrackDecoder(track, ps)
			if oerr != nil {
				reason = "error"
				slog.Warn("zat seek reopen", "node", e.nodeID, "err", oerr)
				break
			}
			dec = nd
			startSample = target
			if serr := s.reSetup(e, ps, track, target); serr != nil {
				reason = "error"
				slog.Warn("zat seek setup", "node", e.nodeID, "err", serr)
				break
			}
			continue
		}
		reason = ar
		break
	}
	ps.listening.gate(false)
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
	e.mqa = false
	e.dop = false
	e.dsdRate = 0
	e.converted = false
	e.bitPerfect = false
	e.mu.Unlock()
	close(ps.done)
	if reason == "eof" {
		go s.autoAdvance(e.nodeID, ps.trackID)
	}
}

// awaitSetup sends SETUP and waits for its acknowledgement.
func (s *Server) awaitSetup(e *endpoint, ps *playState, setup zat.Setup, ctx context.Context) (zat.SetupAck, error) {
	ackCh := make(chan zat.SetupAck, 1)
	e.mu.Lock()
	e.setupAck = ackCh
	e.mu.Unlock()
	if err := e.control(zat.TypeSetup, setup); err != nil {
		return zat.SetupAck{}, err
	}
	select {
	case ack := <-ackCh:
		return ack, nil
	case <-ps.ctx.Done():
		return zat.SetupAck{}, errors.New("播放被新请求取代")
	case <-time.After(8 * time.Second):
		return zat.SetupAck{}, errors.New("终端 SETUP 超时")
	case <-ctx.Done():
		return zat.SetupAck{}, ctx.Err()
	}
}

func setupFailure(ack zat.SetupAck, err error) error {
	if err != nil {
		return err
	}
	if !ack.OK {
		return fmt.Errorf("终端拒绝播放：%s", ack.Detail)
	}
	return fmt.Errorf("终端未能建立位完美输出：%s", ack.Detail)
}

// openTrackFile opens the decoder, limiting CUE virtual tracks to their window.
func openTrackFile(path string, track library.Track) (decode.Decoder, error) {
	if track.CueEnd > track.CueStart {
		return decode.OpenSegment(path, track.CueStart, track.CueEnd)
	}
	return decode.Open(path)
}

// openTrackDecoder reopens the current track for seek, preserving the ffmpeg
// PCM fallback choice once DoP was rejected.
func (s *Server) openTrackDecoder(track library.Track, ps *playState) (decode.Decoder, error) {
	path, err := s.lib.PlaybackPath(track.ID)
	if err != nil {
		return nil, err
	}
	if ps.converted {
		return openFallbackPCM(path, ps.rate, track.Channels, track.DurationMS)
	}
	return openTrackFile(path, track)
}

// reSetup rebuilds the terminal's render session at a seek target: a fresh
// SETUP drops stale buffered PCM, then PLAY with StartPTS skips any decoder
// lead-in. The session id and history are unchanged.
func (s *Server) reSetup(e *endpoint, ps *playState, track library.Track, target uint64) error {
	ackCh := make(chan zat.SetupAck, 1)
	e.mu.Lock()
	e.setupAck = ackCh
	e.mu.Unlock()
	// The terminal drops its old queue on re-SETUP; restart the pacing window.
	e.resetPacing(ps.rate * track.Channels * ps.format.BytesPerSample() / 1000)
	setup := zat.Setup{
		SessionID: ps.sessionID, TrackID: track.ID, Format: ps.format, Rate: ps.rate,
		Channels: track.Channels, PrebufferMS: 200, ALSADevice: ps.device,
		TotalSamples: ps.totalSamples, MQA: ps.mqa, DoP: ps.dop, DSDRate: ps.dsdRate,
	}
	if err := e.control(zat.TypeSetup, setup); err != nil {
		return err
	}
	select {
	case ack := <-ackCh:
		if !ack.OK {
			return fmt.Errorf("终端拒绝播放：%s", ack.Detail)
		}
		if (ps.mqa || ps.dop) && !ack.BitPerfect {
			return fmt.Errorf("终端未能建立位完美输出：%s", ack.Detail)
		}
		e.mu.Lock()
		e.bitPerfect = ack.BitPerfect
		e.mu.Unlock()
	case <-ps.ctx.Done():
		return ps.ctx.Err()
	case <-time.After(8 * time.Second):
		return errors.New("终端 SETUP 超时")
	}
	if err := e.control(zat.TypePlay, zat.Play{SessionID: ps.sessionID, StartPTS: target}); err != nil {
		return err
	}
	state := zat.StatePlaying
	if ps.isPaused() {
		if err := e.control(zat.TypePause, zat.Pause{SessionID: ps.sessionID}); err != nil {
			return err
		}
		state = zat.StatePaused
	}
	ps.listening.gate(state == zat.StatePlaying)
	e.mu.Lock()
	e.pts = target
	e.state = state
	e.mu.Unlock()
	return nil
}

// awaitPlaybackEnd blocks after the source finished sending until the terminal
// finishes playing (STATUS PTS reaches total_samples or it leaves the playing
// state), a seek restarts the stream, the user stops, or a grace deadline
// passes. It returns the teardown reason ("eof", "stopped" or "seek").
func (s *Server) awaitPlaybackEnd(e *endpoint, ps *playState, track library.Track) string {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var deadline time.Time
	lastPTS := ^uint64(0)
	for {
		e.mu.Lock()
		pts, state := e.pts, e.state
		e.mu.Unlock()
		// Extend the grace deadline only while the terminal clock advances, so a
		// seek back near the start gets full remaining time but a stalled
		// terminal still times out.
		if pts != lastPTS {
			lastPTS = pts
			remaining := time.Duration(track.DurationMS) * time.Millisecond
			if ps.totalSamples > 0 && ps.rate > 0 && pts < ps.totalSamples {
				remaining = time.Duration((ps.totalSamples-pts)*1000/uint64(ps.rate)) * time.Millisecond
			}
			deadline = time.Now().Add(remaining + 5*time.Second)
		}
		ps.mu.Lock()
		wake := ps.seekWake
		seeking := ps.seeking
		ps.mu.Unlock()
		// A seek that landed before this capture closed the previous wake
		// channel; catching it here avoids missing it when the tick wins the
		// select race against the close.
		if seeking {
			return "seek"
		}
		select {
		case <-ps.ctx.Done():
			return "stopped"
		case <-wake:
			return "seek"
		case <-tick.C:
			if state == zat.StateStopped || state == zat.StateIdle {
				return "eof"
			}
			if ps.totalSamples > 0 && pts >= ps.totalSamples {
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

// ErrNoSession is returned when a transport control targets a terminal with no
// active push-playback session.
var ErrNoSession = errors.New("当前没有正在播放的会话")

// activePlay returns the endpoint and its live play state, or an error.
func (s *Server) activePlay(nodeID string) (*endpoint, *playState, error) {
	e := s.endpoint(nodeID)
	if e == nil {
		return nil, nil, ErrNotConnected
	}
	e.mu.Lock()
	ps := e.play
	e.mu.Unlock()
	if ps == nil {
		return nil, nil, ErrNoSession
	}
	return e, ps, nil
}

// Pause holds playback: the terminal keeps its ALSA handle but discards incoming
// PCM, so the server gates its pump too. Idempotent.
func (s *Server) Pause(nodeID string) error {
	e, ps, err := s.activePlay(nodeID)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	if ps.paused {
		ps.mu.Unlock()
		return nil
	}
	ps.paused = true
	ps.resumeCh = make(chan struct{})
	ps.mu.Unlock()
	ps.listening.gate(false)
	if err := e.control(zat.TypePause, zat.Pause{SessionID: ps.sessionID}); err != nil {
		return err
	}
	e.mu.Lock()
	e.state = zat.StatePaused
	e.mu.Unlock()
	return nil
}

// Resume releases the pause gate and continues streaming from the held position.
func (s *Server) Resume(nodeID string) error {
	s.libraryGate.RLock()
	defer s.libraryGate.RUnlock()
	e, ps, err := s.activePlay(nodeID)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	if !ps.paused {
		ps.mu.Unlock()
		return nil
	}
	ps.mu.Unlock()
	// PLAY must reach the terminal before the pump is released: audio that
	// overtakes it is discarded while the terminal still considers itself paused.
	if err := e.control(zat.TypePlay, zat.Play{SessionID: ps.sessionID}); err != nil {
		return err
	}
	ps.mu.Lock()
	if ps.paused {
		ps.paused = false
		close(ps.resumeCh)
	}
	ps.mu.Unlock()
	ps.listening.gate(true)
	e.mu.Lock()
	e.state = zat.StatePlaying
	e.mu.Unlock()
	return nil
}

// Seek jumps to an absolute sample position (forward or backward). The pump
// restarts decoding from the target with a fresh SETUP so stale terminal
// buffers are dropped; history and auto-advance are unaffected.
func (s *Server) Seek(nodeID string, samples uint64) error {
	e, ps, err := s.activePlay(nodeID)
	if err != nil {
		return err
	}
	if ps.totalSamples > 0 && samples > ps.totalSamples {
		samples = ps.totalSamples
	}
	ps.listening.gate(false)
	ps.requestSeek(samples)
	return e.control(zat.TypeSeek, zat.Seek{SessionID: ps.sessionID, PTS: samples})
}

// SetVolume sets the terminal's soft volume (0..100). Works with or without an
// active session; the value is tracked server-side for the console.
func (s *Server) SetVolume(nodeID string, volume int) error {
	e := s.endpoint(nodeID)
	if e == nil {
		return ErrNotConnected
	}
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	e.mu.Lock()
	if e.play != nil && e.play.mqa {
		e.mu.Unlock()
		return ErrMQALocked
	}
	if e.play != nil && e.play.dop {
		e.mu.Unlock()
		return ErrDoPLocked
	}
	sid := ""
	if e.play != nil {
		sid = e.play.sessionID
	}
	e.volume = volume
	e.mu.Unlock()
	return e.control(zat.TypeVolume, zat.Volume{SessionID: sid, Volume: volume})
}

// Status reports the live playback state of a terminal.
func (s *Server) Status(nodeID string) (PlayStatus, error) {
	st := PlayStatus{NodeID: nodeID, State: string(zat.StateIdle), Devices: []string{}, Volume: 100}
	st.SleepStopAt = s.SleepStopAt(nodeID)
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
	st.Volume = e.volume
	st.MQA = e.mqa
	st.BitPerfect = e.bitPerfect
	st.MQACapable = e.caps.MQA
	st.DoP = e.dop
	st.DSDRate = e.dsdRate
	st.DoPCapable = e.caps.DoP
	st.Converted = e.converted
	if e.play != nil {
		st.SessionID = e.play.sessionID
		st.Playing = e.state == zat.StatePlaying
	}
	if len(e.caps.ALSADevices) > 0 {
		st.Devices = e.caps.ALSADevices
	}
	if len(e.caps.ALSADeviceInfo) > 0 {
		st.DeviceInfo = e.caps.ALSADeviceInfo
	}
	e.mu.Unlock()
	return st, nil
}

// openFallbackPCM is a seam for tests; production shells out to ffmpeg.
var openFallbackPCM = decode.OpenFFmpegPCM

// mqaCandidate limits the sample scan to stereo PCM at rates MQA delivers.
func mqaCandidate(t library.Track) bool {
	switch t.Codec {
	case "flac", "pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm":
	default:
		return false
	}
	if t.Channels != 2 {
		return false
	}
	switch t.Rate {
	case 44100, 48000, 88200, 96000, 176400, 192000:
		return true
	}
	return false
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
