package zatserver

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hifihub/zat"
)

// ctrlEvent is one frame observed by the controllable fake endpoint.
type ctrlEvent struct {
	typ      zat.Type
	setup    zat.Setup
	play     zat.Play
	seek     zat.Seek
	volume   zat.Volume
	audioPTS uint64
	audioLen int
}

// runCtrlEndpoint authenticates and stays connected across sessions, auto-ACKing
// every SETUP and forwarding observed frames to events. AUDIO is answered with a
// fixed PTS=0 STATUS so the source's drain wait keeps the session alive (the
// test controls lifetime via Stop/Seek rather than a simulated playback clock).
func runCtrlEndpoint(t *testing.T, addr, nodeID, secret string, ready chan<- struct{}, events chan<- ctrlEvent) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Errorf("dial: %v", err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash(secret), AgentVersion: "test", Caps: zat.Caps{MaxChannels: 2}}
	f, _ := zat.ControlFrame(zat.TypeHello, 1, hello)
	if err := zat.WriteFrame(conn, f); err != nil {
		t.Errorf("hello: %v", err)
		return
	}
	ack, err := zat.ReadFrame(conn)
	if err != nil || ack.Type != zat.TypeHelloAck {
		t.Errorf("hello ack: %v %s", err, ack.Type)
		return
	}
	ready <- struct{}{}
	var mu sync.Mutex
	var wmu sync.Mutex // serializes frame writes (STATUS ticker vs SETUP_ACK)
	var sid string
	paused := false
	// Periodic STATUS like the real agent's statusLoop, so the server sees
	// pause/state convergence even when no audio is flowing.
	stopStatus := make(chan struct{})
	defer close(stopStatus)
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stopStatus:
				return
			case <-tk.C:
				mu.Lock()
				state := "playing"
				if paused {
					state = "paused"
				}
				st := zat.Status{SessionID: sid, State: state, PTS: 0, BufferMS: 100}
				mu.Unlock()
				stf, _ := zat.ControlFrame(zat.TypeStatus, 3, st)
				wmu.Lock()
				err := zat.WriteFrame(conn, stf)
				wmu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	for {
		fr, err := zat.ReadFrame(conn)
		if err != nil {
			return
		}
		switch fr.Type {
		case zat.TypeSetup:
			var su zat.Setup
			_ = json.Unmarshal(fr.Payload, &su)
			mu.Lock()
			sid = su.SessionID
			paused = false
			mu.Unlock()
			sa := zat.SetupAck{SessionID: su.SessionID, OK: true,
				Opened: zat.OpenedParams{Format: su.Format, Rate: su.Rate, Channels: su.Channels}, BufferFrames: 1024}
			sf, _ := zat.ControlFrame(zat.TypeSetupAck, 2, sa)
			wmu.Lock()
			_ = zat.WriteFrame(conn, sf)
			wmu.Unlock()
			events <- ctrlEvent{typ: zat.TypeSetup, setup: su}
		case zat.TypePlay:
			var pl zat.Play
			_ = json.Unmarshal(fr.Payload, &pl)
			mu.Lock()
			paused = false
			mu.Unlock()
			events <- ctrlEvent{typ: zat.TypePlay, play: pl}
		case zat.TypePause:
			mu.Lock()
			paused = true
			mu.Unlock()
			events <- ctrlEvent{typ: zat.TypePause}
		case zat.TypeSeek:
			var sk zat.Seek
			_ = json.Unmarshal(fr.Payload, &sk)
			events <- ctrlEvent{typ: zat.TypeSeek, seek: sk}
		case zat.TypeVolume:
			var v zat.Volume
			_ = json.Unmarshal(fr.Payload, &v)
			events <- ctrlEvent{typ: zat.TypeVolume, volume: v}
		case zat.TypeAudio:
			ap, err := zat.DecodeAudio(fr.Payload)
			if err != nil {
				return
			}
			events <- ctrlEvent{typ: zat.TypeAudio, audioPTS: ap.PTS, audioLen: len(ap.PCM)}
		case zat.TypeStop:
			events <- ctrlEvent{typ: zat.TypeStop}
			return
		case zat.TypeTeardown:
			return
		}
	}
}

func waitState(t *testing.T, h *harness, nodeID, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, _ := h.srv.Status(nodeID)
		if st.State == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := h.srv.Status(nodeID)
	t.Fatalf("state = %q want %q", st.State, want)
}

func waitEvent(t *testing.T, events <-chan ctrlEvent, typ zat.Type, timeout time.Duration) ctrlEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-events:
			if ev.typ == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

// drainEvents empties pending events for dur (e.g. in-flight audio after pause).
func drainEvents(events <-chan ctrlEvent, dur time.Duration) {
	deadline := time.After(dur)
	for {
		select {
		case <-events:
		case <-deadline:
			return
		}
	}
}

// collectEvents gathers events arriving within dur, preserving order. Used for
// seek assertions where SEEK/SETUP/PLAY frames race each other.
func collectEvents(events <-chan ctrlEvent, dur time.Duration) []ctrlEvent {
	var out []ctrlEvent
	deadline := time.After(dur)
	for {
		select {
		case ev := <-events:
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
}

func findEvent(evs []ctrlEvent, typ zat.Type) (ctrlEvent, int) {
	for i, ev := range evs {
		if ev.typ == typ {
			return ev, i
		}
	}
	return ctrlEvent{}, -1
}

func expectNoEvent(t *testing.T, events <-chan ctrlEvent, typ zat.Type, dur time.Duration) {
	t.Helper()
	deadline := time.After(dur)
	for {
		select {
		case ev := <-events:
			if ev.typ == typ {
				t.Fatalf("unexpected %s during quiet window", typ)
			}
		case <-deadline:
			return
		}
	}
}

func TestPauseResumeGate(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-pause", true)
	trackID, _ := h.addWAVTrack(t, 200000)

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpoint(t, h.srv.Addr(), nodeID, secret, ready, events)
	<-ready

	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	waitEvent(t, events, zat.TypeAudio, 3*time.Second)

	if err := h.srv.Pause(nodeID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	waitEvent(t, events, zat.TypePause, 3*time.Second)
	waitState(t, h, nodeID, string(zat.StatePaused), 3*time.Second)
	drainEvents(events, 200*time.Millisecond)
	expectNoEvent(t, events, zat.TypeAudio, 400*time.Millisecond)

	if err := h.srv.Resume(nodeID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	waitEvent(t, events, zat.TypeAudio, 3*time.Second)
	waitState(t, h, nodeID, string(zat.StatePlaying), 3*time.Second)

	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
}

func TestSeekBothDirections(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-seek", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	if _, err := h.st.DB.Exec(`UPDATE tracks SET path=? WHERE id=?`, filepath.Join(t.TempDir(), "untrusted.wav"), trackID); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpoint(t, h.srv.Addr(), nodeID, secret, ready, events)
	<-ready

	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	waitEvent(t, events, zat.TypeAudio, 3*time.Second)

	// Pause first so no stale audio races the seek assertions.
	if err := h.srv.Pause(nodeID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	waitEvent(t, events, zat.TypePause, 3*time.Second)
	drainEvents(events, 200*time.Millisecond)

	// Forward seek while paused: re-SETUP + PLAY(StartPTS) + re-PAUSE, no audio.
	if err := h.srv.Seek(nodeID, 100000); err != nil {
		t.Fatalf("Seek forward: %v", err)
	}
	evs := collectEvents(events, 600*time.Millisecond)
	if _, i := findEvent(evs, zat.TypeSeek); i < 0 {
		t.Fatalf("no SEEK frame in %+v", evs)
	}
	su, si := findEvent(evs, zat.TypeSetup)
	if si < 0 {
		t.Fatalf("no re-SETUP in %+v", evs)
	}
	if su.setup.TotalSamples != 200000 {
		t.Fatalf("re-SETUP total samples wrong: %+v", su.setup)
	}
	pl, pi := findEvent(evs, zat.TypePlay)
	if pi < 0 || pl.play.StartPTS != 100000 {
		t.Fatalf("PLAY StartPTS wrong: %+v", evs)
	}
	if _, pa := findEvent(evs, zat.TypePause); pa < 0 || si > pi || pi > pa {
		t.Fatalf("expected SETUP<PLAY<PAUSE order: %+v", evs)
	}
	if _, ai := findEvent(evs, zat.TypeAudio); ai >= 0 {
		t.Fatalf("audio leaked while paused after seek: %+v", evs)
	}

	if err := h.srv.Resume(nodeID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	ev := waitEvent(t, events, zat.TypeAudio, 3*time.Second)
	if ev.audioPTS != 100000 {
		t.Fatalf("first audio PTS after forward seek = %d want 100000", ev.audioPTS)
	}

	// Backward seek: same restart path, audio resumes at the earlier target.
	if err := h.srv.Pause(nodeID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	waitEvent(t, events, zat.TypePause, 3*time.Second)
	drainEvents(events, 200*time.Millisecond)
	if err := h.srv.Seek(nodeID, 5000); err != nil {
		t.Fatalf("Seek backward: %v", err)
	}
	evs = collectEvents(events, 600*time.Millisecond)
	if _, i := findEvent(evs, zat.TypeSetup); i < 0 {
		t.Fatalf("no re-SETUP for backward seek: %+v", evs)
	}
	pl, pi = findEvent(evs, zat.TypePlay)
	if pi < 0 || pl.play.StartPTS != 5000 {
		t.Fatalf("backward PLAY StartPTS wrong: %+v", evs)
	}
	if err := h.srv.Resume(nodeID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	ev = waitEvent(t, events, zat.TypeAudio, 3*time.Second)
	if ev.audioPTS != 5000 {
		t.Fatalf("first audio PTS after backward seek = %d want 5000", ev.audioPTS)
	}

	// Seeks must not record extra history rows.
	var n int
	if err := h.st.DB.QueryRow(`SELECT COUNT(*) FROM play_history WHERE node_id=?`, nodeID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("history rows = %d want 1", n)
	}

	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
}

func TestSeekDuringDrain(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-drain", true)
	trackID, _ := h.addWAVTrack(t, 40000) // 3 chunks: source EOFs quickly
	if _, err := h.st.DB.Exec(`UPDATE tracks SET path=? WHERE id=?`, filepath.Join(t.TempDir(), "untrusted.wav"), trackID); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpoint(t, h.srv.Addr(), nodeID, secret, ready, events)
	<-ready

	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	for i := 0; i < 3; i++ {
		waitEvent(t, events, zat.TypeAudio, 3*time.Second)
	}
	// Source finished sending; the session lives in awaitPlaybackEnd (PTS=0).
	expectNoEvent(t, events, zat.TypeAudio, 300*time.Millisecond)

	if err := h.srv.Seek(nodeID, 20000); err != nil {
		t.Fatalf("Seek during drain: %v", err)
	}
	waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	pl := waitEvent(t, events, zat.TypePlay, 3*time.Second)
	if pl.play.StartPTS != 20000 {
		t.Fatalf("PLAY StartPTS = %d want 20000", pl.play.StartPTS)
	}
	ev := waitEvent(t, events, zat.TypeAudio, 3*time.Second)
	if ev.audioPTS != 20000 {
		t.Fatalf("audio after drain seek PTS = %d want 20000", ev.audioPTS)
	}
	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
}

func TestVolumeAndNoSession(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-vol", true)
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 64)
	go runCtrlEndpoint(t, h.srv.Addr(), nodeID, secret, ready, events)
	<-ready

	if err := h.srv.Pause(nodeID); err != ErrNoSession {
		t.Fatalf("Pause idle: got %v want ErrNoSession", err)
	}
	if err := h.srv.Seek(nodeID, 0); err != ErrNoSession {
		t.Fatalf("Seek idle: got %v want ErrNoSession", err)
	}

	if err := h.srv.SetVolume(nodeID, 40); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	ev := waitEvent(t, events, zat.TypeVolume, 3*time.Second)
	if ev.volume.Volume != 40 || ev.volume.SessionID != "" {
		t.Fatalf("VOLUME payload wrong: %+v", ev.volume)
	}
	st, _ := h.srv.Status(nodeID)
	if st.Volume != 40 {
		t.Fatalf("status volume = %d want 40", st.Volume)
	}
	if err := h.srv.SetVolume(nodeID, 150); err != nil {
		t.Fatalf("SetVolume clamp: %v", err)
	}
	ev = waitEvent(t, events, zat.TypeVolume, 3*time.Second)
	if ev.volume.Volume != 100 {
		t.Fatalf("volume not clamped: %+v", ev.volume)
	}
}
