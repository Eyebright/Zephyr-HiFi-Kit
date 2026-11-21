package zatserver

import (
	"context"
	"encoding/json"
	"errors"
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

func TestEnqueueManyOrderAndAtomicValidation(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "batch", true)
	a, b, _, _ := h.addTwoTracks(t)
	ctx := context.Background()
	if err := h.srv.SetQueue(nodeID, []string{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	q, _, err := h.srv.EnqueueMany(ctx, nodeID, []string{b, a}, true)
	if err != nil || len(q.Tracks) != 4 || q.Position != 0 {
		t.Fatalf("insert next: %+v %v", q, err)
	}
	for i, id := range []string{a, b, a, b} {
		if q.Tracks[i].TrackID != id {
			t.Fatalf("wrong order at %d", i)
		}
	}
	if _, _, err := h.srv.EnqueueMany(ctx, nodeID, []string{a, "missing"}, false); err == nil {
		t.Fatal("invalid batch accepted")
	}
	q, err = h.srv.Queue(nodeID)
	if err != nil || len(q.Tracks) != 4 {
		t.Fatalf("invalid batch changed queue: %+v %v", q, err)
	}
	q, _, err = h.srv.EnqueueMany(ctx, nodeID, []string{a, b}, false)
	if err != nil || len(q.Tracks) != 6 || q.Tracks[4].TrackID != a || q.Tracks[5].TrackID != b {
		t.Fatalf("append: %+v %v", q, err)
	}
	if _, _, err := h.srv.EnqueueMany(ctx, nodeID, nil, false); err == nil {
		t.Fatal("empty batch accepted")
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

// --- playback queue modes ---

// addTracks scans one mount holding the named WAVs and returns title->track ID.
func (h *harness) addTracks(t *testing.T, names ...string) map[string]string {
	t.Helper()
	mountID, target := h.tempMount(t)
	dir := filepath.Join(target, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		writeWAV(t, filepath.Join(dir, name+".wav"), 44100, 2, 16, rampPCM(2000+i*100, 2, 2))
	}
	if _, err := h.lib.Scan(context.Background(), mountID); err != nil {
		t.Fatal(err)
	}
	tracks, err := h.lib.ListTracks(mountID)
	if err != nil || len(tracks) != len(names) {
		t.Fatalf("scan: %v len=%d", err, len(tracks))
	}
	out := make(map[string]string, len(tracks))
	for _, tr := range tracks {
		out[tr.Title] = tr.ID
	}
	for _, name := range names {
		if out[name] == "" {
			t.Fatalf("track %q not scanned: %+v", name, tracks)
		}
	}
	return out
}

// storedQueueMeta reads the raw persisted mode, shuffle order and position.
func storedQueueMeta(t *testing.T, h *harness, nodeID string) (mode string, order []string, pos int) {
	t.Helper()
	var orderJSON string
	if err := h.st.DB.QueryRow(`SELECT mode,shuffle_order,position FROM node_queue WHERE node_id=?`, nodeID).
		Scan(&mode, &orderJSON, &pos); err != nil {
		t.Fatalf("read queue row: %v", err)
	}
	if err := json.Unmarshal([]byte(orderJSON), &order); err != nil {
		t.Fatalf("stored shuffle order: %v", err)
	}
	return mode, order, pos
}

func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// queuePosition returns the persisted cursor and the track it points at.
func queuePosition(t *testing.T, h *harness, nodeID string) (int, string, QueueState) {
	t.Helper()
	q, err := h.srv.Queue(nodeID)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if q.Position < 0 || q.Position >= len(q.Tracks) {
		return q.Position, "", q
	}
	return q.Position, q.Tracks[q.Position].TrackID, q
}

func TestQueueModePersistenceAndLegacyRows(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "mode", true)
	ids := h.addTracks(t, "a", "b", "c")
	list := []string{ids["a"], ids["b"], ids["c"]}
	if err := h.srv.SetQueue(nodeID, list, 0); err != nil {
		t.Fatal(err)
	}

	// New queues default to sequence.
	q, err := h.srv.Queue(nodeID)
	if err != nil || q.Mode != QueueModeSequence || len(q.ShuffleOrder) != 0 {
		t.Fatalf("default mode: %+v %v", q, err)
	}
	for _, m := range []string{QueueModeSequence, QueueModeLoopAll, QueueModeLoopOne, QueueModeShuffle} {
		if !ValidQueueMode(m) {
			t.Fatalf("ValidQueueMode rejected %q", m)
		}
	}
	if ValidQueueMode("bogus") || ValidQueueMode("") {
		t.Fatal("ValidQueueMode accepted an unknown mode")
	}

	// Enabling shuffle freezes a permutation of the current queue.
	q, err = h.srv.SetQueueMode(nodeID, QueueModeShuffle)
	if err != nil || q.Mode != QueueModeShuffle || len(q.ShuffleOrder) != 3 || !sameMultiset(q.ShuffleOrder, list) {
		t.Fatalf("enable shuffle: %+v %v", q, err)
	}
	mode, order, _ := storedQueueMeta(t, h, nodeID)
	if mode != QueueModeShuffle || !equalStrings(order, q.ShuffleOrder) {
		t.Fatalf("shuffle not persisted: mode=%q order=%v", mode, order)
	}

	// A repeated shuffle request keeps the frozen order.
	again, err := h.srv.SetQueueMode(nodeID, QueueModeShuffle)
	if err != nil || !equalStrings(again.ShuffleOrder, order) {
		t.Fatalf("frozen order changed: %v %v", again.ShuffleOrder, err)
	}

	// Leaving shuffle clears the order.
	q, err = h.srv.SetQueueMode(nodeID, QueueModeLoopOne)
	if err != nil || q.Mode != QueueModeLoopOne || len(q.ShuffleOrder) != 0 {
		t.Fatalf("loop_one mode: %+v %v", q, err)
	}
	if _, err := h.srv.SetQueueMode(nodeID, "bogus"); !errors.Is(err, ErrQueueMode) {
		t.Fatalf("expected ErrQueueMode, got %v", err)
	}

	// A queue edit keeps the mode but drops a now-stale shuffle order.
	if _, err := h.srv.SetQueueMode(nodeID, QueueModeShuffle); err != nil {
		t.Fatal(err)
	}
	if err := h.srv.SetQueue(nodeID, []string{ids["a"], ids["b"], ids["c"], ids["a"]}, 0); err != nil {
		t.Fatal(err)
	}
	q, err = h.srv.Queue(nodeID)
	if err != nil || q.Mode != QueueModeShuffle || len(q.ShuffleOrder) != 0 {
		t.Fatalf("stale order survived a queue edit: %+v %v", q, err)
	}

	// Rows written without the mode column behave as sequence.
	if _, err := h.st.DB.Exec(`DELETE FROM node_queue WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	idsJSON, _ := json.Marshal(list)
	if _, err := h.st.DB.Exec(`INSERT INTO node_queue(node_id,track_ids,position,updated_at) VALUES(?,?,?,?)`,
		nodeID, string(idsJSON), 1, 7); err != nil {
		t.Fatal(err)
	}
	q, err = h.srv.Queue(nodeID)
	if err != nil || q.Mode != QueueModeSequence || len(q.ShuffleOrder) != 0 || q.Position != 1 || len(q.Tracks) != 3 {
		t.Fatalf("legacy row: %+v %v", q, err)
	}
	// Editing a legacy row persists the default mode explicitly.
	if err := h.srv.SetQueue(nodeID, list, 0); err != nil {
		t.Fatal(err)
	}
	if mode, order, _ = storedQueueMeta(t, h, nodeID); mode != QueueModeSequence || len(order) != 0 {
		t.Fatalf("legacy edit persisted mode=%q order=%v", mode, order)
	}
}

func TestQueueModeSequenceStopsAtEnd(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "seq", true)
	ids := h.addTracks(t, "a", "b")
	list := []string{ids["a"], ids["b"]}
	if err := h.srv.SetQueue(nodeID, list, 1); err != nil {
		t.Fatal(err)
	}

	// Manual next at the tail of a sequence queue stops.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrQueueEnd) {
		t.Fatalf("expected ErrQueueEnd, got %v", err)
	}
	// Natural EOF at the tail stops too.
	h.srv.autoAdvance(nodeID, ids["b"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("sequence wrapped at EOF: pos=%d track=%s", pos, tr)
	}

	// Sequence still advances normally before the end.
	if _, err := h.srv.QueueJump(context.Background(), nodeID, 0); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("jump: %v", err)
	}
	h.srv.autoAdvance(nodeID, ids["a"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("sequence did not advance: pos=%d track=%s", pos, tr)
	}
	// A finished track that no longer matches the cursor suppresses the advance.
	h.srv.autoAdvance(nodeID, ids["a"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("stale finish advanced the queue: pos=%d track=%s", pos, tr)
	}
}

func TestQueueModeLoopAllWraps(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "loopall", true)
	ids := h.addTracks(t, "a", "b", "c")
	list := []string{ids["a"], ids["b"], ids["c"]}
	if err := h.srv.SetQueue(nodeID, list, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.SetQueueMode(nodeID, QueueModeLoopAll); err != nil {
		t.Fatal(err)
	}

	// Manual next at the tail wraps to the head.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("loop_all next: %v", err)
	}
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 0 || tr != ids["a"] {
		t.Fatalf("loop_all next did not wrap: pos=%d track=%s", pos, tr)
	}
	// Manual prev at the head wraps to the tail.
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("loop_all prev: %v", err)
	}
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 2 || tr != ids["c"] {
		t.Fatalf("loop_all prev did not wrap: pos=%d track=%s", pos, tr)
	}

	// Natural EOF wraps as well.
	h.srv.autoAdvance(nodeID, ids["c"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 0 || tr != ids["a"] {
		t.Fatalf("loop_all EOF did not wrap: pos=%d track=%s", pos, tr)
	}
	h.srv.autoAdvance(nodeID, ids["a"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("loop_all EOF did not advance: pos=%d track=%s", pos, tr)
	}
	// Stale finish is ignored.
	h.srv.autoAdvance(nodeID, ids["c"])
	if pos, _, _ := queuePosition(t, h, nodeID); pos != 1 {
		t.Fatalf("loop_all advanced on a stale finish: pos=%d", pos)
	}
	if mode, _, pos := storedQueueMeta(t, h, nodeID); mode != QueueModeLoopAll || pos != 1 {
		t.Fatalf("loop_all not persisted: mode=%q pos=%d", mode, pos)
	}
}

func TestQueueModeLoopOneRepeatsAndManualNextAdvances(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "loopone", true)
	ids := h.addTracks(t, "a", "b")
	list := []string{ids["a"], ids["b"]}
	if err := h.srv.SetQueue(nodeID, list, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.SetQueueMode(nodeID, QueueModeLoopOne); err != nil {
		t.Fatal(err)
	}

	// Natural EOF repeats the same track without moving the cursor.
	for i := 0; i < 2; i++ {
		h.srv.autoAdvance(nodeID, ids["a"])
		if pos, tr, _ := queuePosition(t, h, nodeID); pos != 0 || tr != ids["a"] {
			t.Fatalf("loop_one EOF moved the cursor: pos=%d track=%s", pos, tr)
		}
	}
	// Manual next still advances instead of sticking.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("loop_one manual next: %v", err)
	}
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("loop_one manual next stuck: pos=%d track=%s", pos, tr)
	}
	// The last entry also repeats naturally.
	h.srv.autoAdvance(nodeID, ids["b"])
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 1 || tr != ids["b"] {
		t.Fatalf("loop_one tail EOF moved the cursor: pos=%d track=%s", pos, tr)
	}
	// Manual next at the tail still stops; prev restarts the head.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrQueueEnd) {
		t.Fatalf("expected ErrQueueEnd, got %v", err)
	}
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("loop_one prev: %v", err)
	}
	if pos, tr, _ := queuePosition(t, h, nodeID); pos != 0 || tr != ids["a"] {
		t.Fatalf("loop_one prev: pos=%d track=%s", pos, tr)
	}
	// A stale finish (queue edited meanwhile) must not replay.
	h.srv.autoAdvance(nodeID, ids["b"])
	if pos, _, _ := queuePosition(t, h, nodeID); pos != 0 {
		t.Fatalf("stale finish replayed: pos=%d", pos)
	}
}

func TestQueueModeShuffleFixedOrderCycle(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "shuffle", true)
	ids := h.addTracks(t, "a", "b", "c", "d", "e")
	list := []string{ids["a"], ids["b"], ids["c"], ids["d"], ids["e"]}
	if err := h.srv.SetQueue(nodeID, list, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.SetQueueMode(nodeID, QueueModeShuffle); err != nil {
		t.Fatal(err)
	}
	q, err := h.srv.Queue(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	frozen := append([]string(nil), q.ShuffleOrder...)
	if len(frozen) != len(list) || !sameMultiset(frozen, list) {
		t.Fatalf("shuffle order is not a permutation of the queue: %v", frozen)
	}

	// Natural EOF walks the frozen order exactly once, with no immediate repeat,
	// then wraps back to the starting track.
	start := q.Tracks[q.Position].TrackID
	at := indexOf(frozen, start)
	cur := start
	visited := make([]string, 0, len(frozen))
	for i := 0; i < len(frozen); i++ {
		h.srv.autoAdvance(nodeID, cur)
		pos, tr, st := queuePosition(t, h, nodeID)
		want := frozen[(indexOf(frozen, cur)+1)%len(frozen)]
		if tr != want || pos != indexOf(list, want) {
			t.Fatalf("shuffle step %d: got pos=%d track=%s want %s", i, pos, tr, want)
		}
		if tr == cur {
			t.Fatalf("immediate repeat of %s inside one shuffle cycle", cur)
		}
		if !equalStrings(st.ShuffleOrder, frozen) {
			t.Fatalf("shuffle order drifted mid-cycle: %v", st.ShuffleOrder)
		}
		visited = append(visited, tr)
		cur = tr
	}
	if cur != start {
		t.Fatalf("shuffle cycle did not return to the start: %s != %s", cur, start)
	}
	expected := make([]string, 0, len(frozen))
	for i := 1; i <= len(frozen); i++ {
		expected = append(expected, frozen[(at+i)%len(frozen)])
	}
	if !equalStrings(visited, expected) {
		t.Fatalf("shuffle cycle order %v != frozen order %v", visited, expected)
	}

	// Manual next/prev follow the same frozen order and wrap.
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("shuffle next: %v", err)
	}
	if _, tr, _ := queuePosition(t, h, nodeID); tr != frozen[(at+1)%len(frozen)] {
		t.Fatalf("shuffle manual next: %s want %s", tr, frozen[(at+1)%len(frozen)])
	}
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("shuffle prev: %v", err)
	}
	if _, tr, _ := queuePosition(t, h, nodeID); tr != start {
		t.Fatalf("shuffle manual prev: %s want %s", tr, start)
	}
	if _, err := h.srv.QueuePrev(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("shuffle prev wrap: %v", err)
	}
	if _, tr, _ := queuePosition(t, h, nodeID); tr != frozen[(at-1+len(frozen))%len(frozen)] {
		t.Fatalf("shuffle manual prev wrap: %s want %s", tr, frozen[(at-1+len(frozen))%len(frozen)])
	}

	// Queue edits invalidate the frozen order; the next advance regenerates one.
	newList := []string{ids["c"], ids["d"], ids["e"]}
	if err := h.srv.SetQueue(nodeID, newList, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, q := queuePosition(t, h, nodeID); q.Mode != QueueModeShuffle || len(q.ShuffleOrder) != 0 {
		t.Fatalf("edited queue kept a stale shuffle order: %+v", q.ShuffleOrder)
	}
	if _, err := h.srv.QueueNext(context.Background(), nodeID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("shuffle next after edit: %v", err)
	}
	_, tr, q := queuePosition(t, h, nodeID)
	if len(q.ShuffleOrder) != len(newList) || !sameMultiset(q.ShuffleOrder, newList) {
		t.Fatalf("lazy shuffle regeneration failed: %v", q.ShuffleOrder)
	}
	if want := q.ShuffleOrder[(indexOf(q.ShuffleOrder, newList[0])+1)%len(newList)]; tr != want {
		t.Fatalf("lazy shuffle next: %s want %s", tr, want)
	}
}
