package zatserver

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/library"
	"hifihub/zat"
)

// tempMount creates a mount row backed by a temp dir (no real CIFS mount).
func (h *harness) tempMount(t *testing.T) (mountID, target string) {
	t.Helper()
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target = t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	return m.ID, target
}

// addTwoTracks scans one mount holding two short WAVs and returns them in
// album-order-independent fashion (looked up by title).
func (h *harness) addTwoTracks(t *testing.T) (a, b string, pcmA, pcmB []byte) {
	t.Helper()
	mountID, target := h.tempMount(t)
	dir := filepath.Join(target, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pcmA = rampPCM(2000, 2, 2)
	pcmB = rampPCM(3000, 2, 2)
	writeWAV(t, filepath.Join(dir, "alpha.wav"), 44100, 2, 16, pcmA)
	writeWAV(t, filepath.Join(dir, "beta.wav"), 44100, 2, 16, pcmB)
	if _, err := h.lib.Scan(context.Background(), mountID); err != nil {
		t.Fatal(err)
	}
	tracks, err := h.lib.ListTracks(mountID)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("scan: %v len=%d", err, len(tracks))
	}
	for _, tr := range tracks {
		switch tr.Title {
		case "alpha":
			a = tr.ID
		case "beta":
			b = tr.ID
		}
	}
	if a == "" || b == "" {
		t.Fatalf("tracks not found: %+v", tracks)
	}
	return a, b, pcmA, pcmB
}

// runEndpointMulti is runEndpoint but stays connected across sessions: each
// STOP delivers one epResult and the loop keeps serving the next SETUP.
func runEndpointMulti(t *testing.T, addr, nodeID, secret string, ready chan<- struct{}, results chan<- epResult) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		results <- epResult{err: err}
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash(secret), AgentVersion: "test", Caps: zat.Caps{MaxChannels: 2}}
	f, _ := zat.ControlFrame(zat.TypeHello, 1, hello)
	if err := zat.WriteFrame(conn, f); err != nil {
		results <- epResult{err: err}
		return
	}
	ack, err := zat.ReadFrame(conn)
	if err != nil || ack.Type != zat.TypeHelloAck {
		results <- epResult{err: err}
		return
	}
	ready <- struct{}{}
	var res epResult
	for {
		fr, err := zat.ReadFrame(conn)
		if err != nil {
			res.err = err
			results <- res
			return
		}
		switch fr.Type {
		case zat.TypeSetup:
			res = epResult{}
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
				results <- res
				return
			}
			res.pcm = append(res.pcm, ap.PCM...)
			res.pts = append(res.pts, ap.PTS)
			st := zat.Status{SessionID: res.setup.SessionID, State: "playing",
				PTS: ap.PTS + uint64(len(ap.PCM)/4), BufferMS: 100}
			stf, _ := zat.ControlFrame(zat.TypeStatus, 3, st)
			_ = zat.WriteFrame(conn, stf)
		case zat.TypeStop:
			res.stop = true
			results <- res
		case zat.TypeError:
			res.err = errUnexpected(zat.TypeError)
			results <- res
			return
		}
	}
}

func nextResult(t *testing.T, results <-chan epResult) epResult {
	t.Helper()
	select {
	case res := <-results:
		if res.err != nil {
			t.Fatalf("endpoint: %v", res.err)
		}
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("endpoint timed out waiting for playback result")
		return epResult{}
	}
}

func TestQueueLifecycleAndAutoAdvance(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-q", true)
	a, b, pcmA, pcmB := h.addTwoTracks(t)

	ready := make(chan struct{}, 1)
	results := make(chan epResult, 8)
	go runEndpointMulti(t, h.srv.Addr(), nodeID, secret, ready, results)
	<-ready

	// Enqueue on an empty queue starts playback immediately.
	q, sessionID, err := h.srv.Enqueue(context.Background(), nodeID, a)
	if err != nil || sessionID == "" {
		t.Fatalf("enqueue start-now: %v session=%q", err, sessionID)
	}
	if len(q.Tracks) != 1 || q.Position != 0 || q.Tracks[0].Title != "alpha" {
		t.Fatalf("queue after enqueue: %+v", q)
	}
	res := nextResult(t, results)
	if string(res.pcm) != string(pcmA) || !res.stop {
		t.Fatalf("first playback wrong: stop=%v bytes=%d", res.stop, len(res.pcm))
	}
	waitPlaying(t, h.nodes, nodeID, false)

	// Appending to a non-empty queue does not interrupt or start playback.
	q, sessionID, err = h.srv.Enqueue(context.Background(), nodeID, b)
	if err != nil || sessionID != "" {
		t.Fatalf("enqueue append: %v session=%q", err, sessionID)
	}
	if len(q.Tracks) != 2 || q.Position != 0 {
		t.Fatalf("queue after append: %+v", q)
	}

	// next -> beta, then next at the end errors without wrapping.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); err != nil {
		t.Fatalf("next: %v", err)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != b || string(res.pcm) != string(pcmB) {
		t.Fatalf("next played wrong track: %s bytes=%d", res.setup.TrackID, len(res.pcm))
	}
	waitPlaying(t, h.nodes, nodeID, false)
	if _, err := h.srv.QueueNext(context.Background(), nodeID); err != ErrQueueEnd {
		t.Fatalf("expected ErrQueueEnd, got %v", err)
	}

	// prev -> alpha; its natural EOF then auto-advances to beta (pos 0 of 2).
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); err != nil {
		t.Fatalf("prev: %v", err)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != a || string(res.pcm) != string(pcmA) {
		t.Fatalf("prev played wrong track: %s", res.setup.TrackID)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != b {
		t.Fatalf("prev playback did not auto-advance: %s", res.setup.TrackID)
	}
	waitPlaying(t, h.nodes, nodeID, false)

	// prev at the head of a single-track queue restarts the current track.
	if err := h.srv.SetQueue(nodeID, []string{a}, 0); err != nil {
		t.Fatalf("set single queue: %v", err)
	}
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); err != nil {
		t.Fatalf("prev at head: %v", err)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != a || string(res.pcm) != string(pcmA) {
		t.Fatalf("prev at head played %s", res.setup.TrackID)
	}
	waitPlaying(t, h.nodes, nodeID, false)

	// Natural EOF auto-advances: play alpha at position 0 of [alpha, beta] and
	// expect beta to start without any explicit next call.
	if err := h.srv.SetQueue(nodeID, []string{a, b}, 0); err != nil {
		t.Fatalf("set queue: %v", err)
	}
	if _, err := h.srv.QueueJump(context.Background(), nodeID, 0); err != nil {
		t.Fatalf("jump: %v", err)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != a {
		t.Fatalf("auto-advance first track: %s", res.setup.TrackID)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != b || string(res.pcm) != string(pcmB) {
		t.Fatalf("auto-advance did not play beta: %s bytes=%d", res.setup.TrackID, len(res.pcm))
	}
	waitPlaying(t, h.nodes, nodeID, false)
	q, err = h.srv.Queue(nodeID)
	if err != nil || q.Position != 1 {
		t.Fatalf("queue position after auto-advance: %+v %v", q, err)
	}

	// A user stop must NOT auto-advance.
	if _, err := h.srv.QueueJump(context.Background(), nodeID, 0); err != nil {
		t.Fatalf("jump 0: %v", err)
	}
	waitPlaying(t, h.nodes, nodeID, true)
	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	res = nextResult(t, results)
	if res.setup.TrackID != a || !res.stop {
		t.Fatalf("stopped playback wrong: %+v", res.setup)
	}
	waitPlaying(t, h.nodes, nodeID, false)
	select {
	case extra := <-results:
		t.Fatalf("user stop auto-advanced: %+v", extra.setup)
	case <-time.After(500 * time.Millisecond):
	}

	// Jump out of range and empty-queue moves are rejected.
	if _, err := h.srv.QueueJump(context.Background(), nodeID, 5); err != ErrQueueIndex {
		t.Fatalf("expected ErrQueueIndex, got %v", err)
	}
	if err := h.srv.SetQueue(nodeID, nil, 0); err != nil {
		t.Fatalf("clear queue: %v", err)
	}
	if _, err := h.srv.QueueNext(context.Background(), nodeID); err != ErrQueueEmpty {
		t.Fatalf("expected ErrQueueEmpty, got %v", err)
	}
	if err := h.srv.SetQueue(nodeID, []string{"trk-missing"}, 0); err == nil {
		t.Fatal("SetQueue accepted an unknown track")
	}
}
