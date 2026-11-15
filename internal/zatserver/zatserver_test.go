package zatserver

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/library"
	"hifihub/server/internal/mount"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
	"hifihub/zat"
)

type noopRunner struct{}

func (noopRunner) Run(context.Context, string, ...string) (string, error) { return "", nil }

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type harness struct {
	srv   *Server
	nodes *node.Service
	lib   *library.Service
	st    *store.Store
}

func setup(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := node.New(st, 45*time.Second)
	lib := library.New(st, mount.NewManager(noopRunner{}, t.TempDir()), "/mnt/zephyr")
	srv := New("127.0.0.1:0", nodes, lib, st)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	t.Cleanup(func() { cancel(); srv.Close(); st.Close() })
	return &harness{srv: srv, nodes: nodes, lib: lib, st: st}
}

func (h *harness) register(t *testing.T, alias string, authorized bool) (nodeID, secret string) {
	t.Helper()
	tok, _, err := h.nodes.IssueToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	n, sec, err := h.nodes.Register(node.RegisterRequest{Token: tok, Alias: alias, Arch: "arm64"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if authorized {
		if err := h.nodes.SetAuthorized(n.NodeID, true); err != nil {
			t.Fatal(err)
		}
	}
	return n.NodeID, sec
}

// addWAVTrack mounts a temp share, writes a ramp WAV, scans and returns the track.
func (h *harness) addWAVTrack(t *testing.T, frames int) (trackID string, pcm []byte) {
	t.Helper()
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	album := filepath.Join(target, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	pcm = rampPCM(frames, 2, 2)
	writeWAV(t, filepath.Join(album, "song.wav"), 44100, 2, 16, pcm)
	if _, err := h.lib.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := h.lib.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("scan: %v len=%d", err, len(tracks))
	}
	return tracks[0].ID, pcm
}

func rampPCM(frames, channels, bps int) []byte {
	out := make([]byte, frames*channels*bps)
	for i := 0; i < frames*channels; i++ {
		v := int16((i * 7) % 32768)
		off := i * bps
		out[off] = byte(v)
		out[off+1] = byte(v >> 8)
	}
	return out
}

func writeWAV(t *testing.T, path string, rate, channels, bits int, pcm []byte) {
	t.Helper()
	blockAlign := channels * bits / 8
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(rate*blockAlign))
	binary.LittleEndian.PutUint16(hdr[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(hdr[34:36], uint16(bits))
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	if err := os.WriteFile(path, append(hdr[:], pcm...), 0o644); err != nil {
		t.Fatal(err)
	}
}

type epResult struct {
	pcm   []byte
	pts   []uint64
	setup zat.Setup
	meta  zat.TrackMeta
	play  bool
	stop  bool
	err   error
}

// runEndpoint connects, authenticates and consumes one playback, returning the
// reassembled PCM. It signals ready after HELLO_ACK and completes on STOP.
func runEndpoint(t *testing.T, addr, nodeID, secret string, ready chan<- struct{}, done chan<- epResult) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		done <- epResult{err: err}
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash(secret), AgentVersion: "test", Caps: zat.Caps{MaxChannels: 2}}
	f, _ := zat.ControlFrame(zat.TypeHello, 1, hello)
	if err := zat.WriteFrame(conn, f); err != nil {
		done <- epResult{err: err}
		return
	}
	ack, err := zat.ReadFrame(conn)
	if err != nil {
		done <- epResult{err: err}
		return
	}
	if ack.Type != zat.TypeHelloAck {
		done <- epResult{err: errUnexpected(ack.Type)}
		return
	}
	ready <- struct{}{}
	var res epResult
	for {
		fr, err := zat.ReadFrame(conn)
		if err != nil {
			res.err = err
			done <- res
			return
		}
		switch fr.Type {
		case zat.TypeSetup:
			_ = json.Unmarshal(fr.Payload, &res.setup)
			sa := zat.SetupAck{SessionID: res.setup.SessionID, OK: true,
				Opened:       zat.OpenedParams{Format: res.setup.Format, Rate: res.setup.Rate, Channels: res.setup.Channels},
				BufferFrames: 1024}
			sf, _ := zat.ControlFrame(zat.TypeSetupAck, 2, sa)
			_ = zat.WriteFrame(conn, sf)
		case zat.TypePlay:
			res.play = true
		case zat.TypeTrackMeta:
			_ = json.Unmarshal(fr.Payload, &res.meta)
		case zat.TypeAudio:
			ap, err := zat.DecodeAudio(fr.Payload)
			if err != nil {
				res.err = err
				done <- res
				return
			}
			res.pcm = append(res.pcm, ap.PCM...)
			res.pts = append(res.pts, ap.PTS)
			// Report playback position like a real terminal so the source can
			// detect completion when PTS reaches total_samples.
			st := zat.Status{SessionID: res.setup.SessionID, State: "playing",
				PTS: ap.PTS + uint64(len(ap.PCM)/4), BufferMS: 100}
			stf, _ := zat.ControlFrame(zat.TypeStatus, 3, st)
			_ = zat.WriteFrame(conn, stf)
		case zat.TypeStop:
			res.stop = true
			done <- res
			return
		case zat.TypeError:
			var ze zat.Error
			_ = json.Unmarshal(fr.Payload, &ze)
			res.err = errUnexpected(zat.TypeError)
			done <- res
			return
		}
	}
}

type errUnexpected zat.Type

func (e errUnexpected) Error() string { return "unexpected frame: " + zat.Type(e).String() }

func waitPlaying(t *testing.T, nodes *node.Service, nodeID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n, err := nodes.Get(nodeID)
		if err == nil && n.Playing == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	n, _ := nodes.Get(nodeID)
	t.Fatalf("playing=%v, want %v", n.Playing, want)
}

func TestPushPlaybackRoundTrip(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep1", true)
	trackID, pcm := h.addWAVTrack(t, 40000) // >64KiB => multi-chunk

	ready := make(chan struct{}, 1)
	done := make(chan epResult, 1)
	go runEndpoint(t, h.srv.Addr(), nodeID, secret, ready, done)
	<-ready

	if !h.srv.Connected(nodeID) {
		t.Fatal("endpoint not registered")
	}
	st0, err := h.srv.Status(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !st0.Connected || st0.Playing {
		t.Fatalf("pre-play status wrong: %+v", st0)
	}
	sessionID, err := h.srv.StartPlay(context.Background(), nodeID, trackID)
	if err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	if sessionID == "" {
		t.Fatal("empty session id")
	}
	waitPlaying(t, h.nodes, nodeID, true)

	var res epResult
	select {
	case res = <-done:
	case <-time.After(12 * time.Second):
		t.Fatal("endpoint timed out")
	}
	if res.err != nil {
		t.Fatalf("endpoint: %v", res.err)
	}
	if !res.play || !res.stop {
		t.Fatalf("missing play/stop: %+v", res)
	}
	if res.setup.Format != zat.S16LE || res.setup.Rate != 44100 || res.setup.Channels != 2 {
		t.Fatalf("setup wrong: %+v", res.setup)
	}
	if res.setup.ALSADevice != "default" {
		t.Fatalf("alsa device wrong: %q", res.setup.ALSADevice)
	}
	if res.meta.Title != "song" || res.meta.Artist != "Artist" || res.meta.Album != "Album" {
		t.Fatalf("track meta wrong: %+v", res.meta)
	}
	if string(res.pcm) != string(pcm) {
		t.Fatalf("PCM round-trip mismatch: got %d bytes want %d", len(res.pcm), len(pcm))
	}
	// 40000 frames * 4 bytes = 160000 bytes => chunks of 16384 frames.
	if len(res.pts) < 2 {
		t.Fatalf("expected multi-chunk, got %d frames", len(res.pts))
	}
	if res.pts[0] != 0 || res.pts[1] != 16384 {
		t.Fatalf("PTS clock wrong: %v", res.pts)
	}
	waitPlaying(t, h.nodes, nodeID, false)

	// The fake endpoint closes after STOP, so the server has unregistered it.
	st1, err := h.srv.Status(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if st1.Playing {
		t.Fatalf("post-stop still playing: %+v", st1)
	}
}

func TestStopCancelsStream(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep2", true)
	trackID, _ := h.addWAVTrack(t, 200000) // large enough to still be streaming

	ready := make(chan struct{}, 1)
	done := make(chan epResult, 1)
	go runEndpoint(t, h.srv.Addr(), nodeID, secret, ready, done)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	waitPlaying(t, h.nodes, nodeID, true)
	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitPlaying(t, h.nodes, nodeID, false)
	select {
	case res := <-done:
		if !res.stop {
			t.Fatalf("expected STOP, got %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no STOP after Stop()")
	}
}

func TestAuthWrongSecret(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "ep3", true)
	conn, err := net.Dial("tcp", h.srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash("wrong")}
	f, _ := zat.ControlFrame(zat.TypeHello, 1, hello)
	if err := zat.WriteFrame(conn, f); err != nil {
		t.Fatal(err)
	}
	fr, err := zat.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if fr.Type != zat.TypeError {
		t.Fatalf("expected ERROR, got %s", fr.Type)
	}
	var ze zat.Error
	_ = json.Unmarshal(fr.Payload, &ze)
	if ze.Code != zat.ErrAuthFailed {
		t.Fatalf("expected auth_failed, got %s", ze.Code)
	}
}

func TestAuthUnauthorized(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep4", false)
	conn, err := net.Dial("tcp", h.srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash(secret)}
	f, _ := zat.ControlFrame(zat.TypeHello, 1, hello)
	_ = zat.WriteFrame(conn, f)
	fr, err := zat.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	var ze zat.Error
	_ = json.Unmarshal(fr.Payload, &ze)
	if fr.Type != zat.TypeError || ze.Code != zat.ErrNotAuthorized {
		t.Fatalf("expected not_authorized, got %s %+v", fr.Type, ze)
	}
}

func TestStartPlayNotConnected(t *testing.T) {
	h := setup(t)
	if _, err := h.srv.StartPlay(context.Background(), "n-missing", "trk-x"); err != ErrNotConnected {
		t.Fatalf("expected ErrNotConnected, got %v", err)
	}
}
