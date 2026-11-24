package zatserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"hifihub/server/internal/decode"
	"hifihub/server/internal/library"
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

type ctrlOpts struct {
	caps          zat.Caps
	bitPerfect    bool
	persistent    bool
	deviceUpdates <-chan []zat.ALSADevice
}

// runCtrlEndpoint authenticates and stays connected across sessions, auto-ACKing
// every SETUP and forwarding observed frames to events. AUDIO is answered with a
// fixed PTS=0 STATUS so the source's drain wait keeps the session alive (the
// test controls lifetime via Stop/Seek rather than a simulated playback clock).
func runCtrlEndpoint(t *testing.T, addr, nodeID, secret string, ready chan<- struct{}, events chan<- ctrlEvent) {
	runCtrlEndpointOpts(t, addr, nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2}}, ready, events)
}

func runCtrlEndpointOpts(t *testing.T, addr, nodeID, secret string, opts ctrlOpts, ready chan<- struct{}, events chan<- ctrlEvent) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Errorf("dial: %v", err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	hello := zat.Hello{NodeID: nodeID, SecretSHA256: hash(secret), AgentVersion: "test", Caps: opts.caps}
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
	devices := slices.Clone(opts.caps.ALSADevices)
	deviceInfo := slices.Clone(opts.caps.ALSADeviceInfo)
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
			case update, ok := <-opts.deviceUpdates:
				if !ok {
					return
				}
				mu.Lock()
				devices = devices[:0]
				deviceInfo = append(deviceInfo[:0], update...)
				for _, d := range update {
					devices = append(devices, d.HW)
				}
				mu.Unlock()
			case <-tk.C:
				mu.Lock()
				state := "playing"
				if paused {
					state = "paused"
				}
				st := zat.Status{SessionID: sid, State: state, PTS: 0, BufferMS: 100,
					ALSADevices: slices.Clone(devices), ALSADeviceInfo: slices.Clone(deviceInfo)}
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
			sa := zat.SetupAck{SessionID: su.SessionID, OK: true, BitPerfect: opts.bitPerfect,
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
			mu.Lock()
			sid = ""
			paused = false
			mu.Unlock()
			if !opts.persistent {
				return
			}
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

func TestMQAPlaybackStrictPath(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-mqa", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	if _, err := h.st.DB.Exec(`UPDATE tracks SET mqa=1, mqa_checked=1 WHERE id=?`, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='hw:0,0' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, MQA: true, ALSADevices: []string{"hw:0,0"}, ALSADeviceInfo: []zat.ALSADevice{{HW: "hw:0,0", Name: "Test DAC"}}}, bitPerfect: true}, ready, events)
	<-ready

	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	su := waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if !su.setup.MQA {
		t.Fatalf("SETUP did not request MQA: %+v", su.setup)
	}
	st, err := h.srv.Status(nodeID)
	if err != nil || !st.MQA || !st.BitPerfect || !st.MQACapable {
		t.Fatalf("status mqa/bitperfect = %+v err=%v", st, err)
	}
	if err := h.srv.SetVolume(nodeID, 40); !errors.Is(err, ErrMQALocked) {
		t.Fatalf("SetVolume during MQA = %v want ErrMQALocked", err)
	}
	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
}

func TestMQARequiresCapabilityAndHW(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-mqa-2", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	if _, err := h.st.DB.Exec(`UPDATE tracks SET mqa=1, mqa_checked=1 WHERE id=?`, trackID); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 64)
	go runCtrlEndpoint(t, h.srv.Addr(), nodeID, secret, ready, events)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); !errors.Is(err, ErrMQACapability) {
		t.Fatalf("StartPlay without MQA cap = %v want ErrMQACapability", err)
	}
}

func TestMQADeviceRequiresHW(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-mqa-3", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	if _, err := h.st.DB.Exec(`UPDATE tracks SET mqa=1, mqa_checked=1 WHERE id=?`, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='default' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 64)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, MQA: true, ALSADevices: []string{"hw:0,0"}, ALSADeviceInfo: []zat.ALSADevice{{HW: "hw:0,0", Name: "Test DAC"}}}, bitPerfect: true}, ready, events)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, trackID); !errors.Is(err, ErrMQADevice) {
		t.Fatalf("StartPlay on default device = %v want ErrMQADevice", err)
	}
}

func TestDSDDoPStrictPath(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-dop", true)
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='hw:0,0' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(target, "DSD")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDSF(t, filepath.Join(dir, "song.dsf"))
	if _, err := h.lib.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := h.lib.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("scan dsd: %v len=%d", err, len(tracks))
	}
	if !tracks[0].Decodable || tracks[0].Codec != "dsd" {
		t.Fatalf("dsd track = %+v", tracks[0])
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, DoP: true, ALSADevices: []string{"hw:0,0"}, ALSADeviceInfo: []zat.ALSADevice{{HW: "hw:0,0", Name: "Test DAC"}}}, bitPerfect: true}, ready, events)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, tracks[0].ID); err != nil {
		t.Fatalf("StartPlay: %v", err)
	}
	su := waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if !su.setup.DoP || su.setup.DSDRate != 2822400 || su.setup.Rate != 176400 || su.setup.Format != zat.S24LE {
		t.Fatalf("DoP setup = %+v", su.setup)
	}
	st, err := h.srv.Status(nodeID)
	if err != nil || !st.DoP || st.DSDRate != 2822400 || !st.BitPerfect || !st.DoPCapable {
		t.Fatalf("DoP status = %+v err=%v", st, err)
	}
	if err := h.srv.SetVolume(nodeID, 40); !errors.Is(err, ErrDoPLocked) {
		t.Fatalf("SetVolume during DoP = %v want ErrDoPLocked", err)
	}
	_ = h.srv.Stop(nodeID)
}

func TestDSDRequiresDoPCapability(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-dop-2", true)
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='hw:0,0' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "DSD"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDSF(t, filepath.Join(target, "DSD", "song.dsf"))
	if _, err := h.lib.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, _ := h.lib.ListTracks(m.ID)
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 64)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0"}}}, ready, events)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, tracks[0].ID); !errors.Is(err, ErrDoPCapability) {
		t.Fatalf("StartPlay without DoP cap = %v want ErrDoPCapability", err)
	}
}

type fakePCMDecoder struct {
	format decode.PCMFormat
	total  uint64
}

func (f *fakePCMDecoder) Read([]byte) (int, error) { return 0, io.EOF }
func (f *fakePCMDecoder) Close() error             { return nil }
func (f *fakePCMDecoder) Format() decode.PCMFormat { return f.format }
func (f *fakePCMDecoder) TotalSamples() uint64     { return f.total }

func TestPlaybackDeviceTargetValidation(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-targets", true)
	otherID, _ := h.register(t, "ep-other", true)
	defaultKey := makeDeviceKey(nodeID, "default")
	dacKey := makeDeviceKey(nodeID, "hw:1,0")
	for _, key := range []string{"missing", "::hw:1,0", makeDeviceKey(otherID, "hw:1,0"), makeDeviceKey(nodeID, ""), nodeID + "::hw:1,0\n"} {
		if _, err := parseDeviceKey(nodeID, key); !errors.Is(err, ErrInvalidDeviceKey) {
			t.Fatalf("parseDeviceKey(%q) = %v", key, err)
		}
	}
	if _, err := h.srv.resolveTarget(nodeID, defaultKey); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("offline explicit target = %v", err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:9,9", true); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 32)
	updates := make(chan []zat.ALSADevice, 1)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{
		caps: zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0", "hw:1,0"},
			ALSADeviceInfo: []zat.ALSADevice{{HW: "hw:0,0", Name: "板载"}, {HW: "hw:1,0", Name: "AUDIO"}}},
		persistent: true, deviceUpdates: updates,
	}, ready, events)
	<-ready
	n, err := h.nodes.Get(nodeID)
	if err != nil || n.Status != "online" {
		t.Fatalf("HELLO did not mark node online: %+v %v", n, err)
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device=? WHERE node_id=?`, "hw:1,0", nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.resolveTarget(nodeID, ""); !errors.Is(err, ErrDeviceNotActivated) {
		t.Fatalf("legacy target bypassed activation: %v", err)
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='default' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.resolveTarget(nodeID, dacKey); !errors.Is(err, ErrDeviceNotActivated) {
		t.Fatalf("inactive DAC = %v", err)
	}
	if _, err := h.srv.resolveTarget(nodeID, makeDeviceKey(nodeID, "hw:9,9")); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("absent DAC = %v", err)
	}
	if _, err := h.srv.resolveTarget(nodeID, defaultKey); err != nil {
		t.Fatalf("default target: %v", err)
	}
	if err := h.nodes.RenameDevice(nodeID, "hw:1,0", "客厅 DAC"); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:1,0", true); err != nil {
		t.Fatal(err)
	}
	if got, err := h.srv.resolveTarget(nodeID, dacKey); err != nil || got.HW != "hw:1,0" {
		t.Fatalf("active DAC = %+v %v", got, err)
	}
	devs := h.srv.ActiveDevices()
	if len(devs) != 2 {
		t.Fatalf("active devices = %+v", devs)
	}
	var dac *PlayDevice
	for i := range devs {
		if devs[i].DeviceKey == dacKey {
			dac = &devs[i]
		}
	}
	if dac == nil || dac.Name != "客厅 DAC" || !dac.Present || !dac.Online || dac.NodeID != nodeID {
		t.Fatalf("active DAC metadata = %+v", dac)
	}
	updates <- []zat.ALSADevice{{HW: "hw:0,0", Name: "板载"}}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		devs = h.srv.ActiveDevices()
		if len(devs) == 1 && devs[0].HW == "default" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(devs) != 1 || devs[0].HW != "default" {
		t.Fatalf("unplugged DAC remained selectable: %+v", devs)
	}
	if _, err := h.srv.resolveTarget(nodeID, dacKey); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("unplugged DAC target = %v", err)
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device=? WHERE node_id=?`, "hw:9,9", nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.resolveTarget(nodeID, ""); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("legacy target bypassed presence: %v", err)
	}
}

func TestPlaybackDeviceSwitchVolumeQueueAndSleepIsolation(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-isolation", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	key0 := makeDeviceKey(nodeID, "hw:0,0")
	key1 := makeDeviceKey(nodeID, "hw:1,0")
	for _, hw := range []string{"hw:0,0", "hw:1,0"} {
		if err := h.nodes.SetDeviceEnabled(nodeID, hw, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.nodes.SetDeviceVolume(nodeID, "hw:0,0", 25); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceVolume(nodeID, "hw:1,0", 70); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{
		caps:       zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0", "hw:1,0"}},
		persistent: true,
	}, ready, events)
	<-ready
	if err := h.srv.SetQueueDevice(nodeID, key0, []string{trackID}, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.srv.SetQueueDevice(nodeID, key1, []string{trackID}, 0); err != nil {
		t.Fatal(err)
	}
	stop0, err := h.srv.SetSleepTimerDevice(nodeID, key0, 15)
	if err != nil {
		t.Fatal(err)
	}
	stop1, err := h.srv.SetSleepTimerDevice(nodeID, key1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if stop0 == 0 || stop1 == 0 || h.srv.SleepStopAtDevice(nodeID, key0) != stop0 || h.srv.SleepStopAtDevice(nodeID, key1) != stop1 {
		t.Fatalf("sleep timers not isolated: %d %d", stop0, stop1)
	}
	if err := h.srv.CancelSleepTimerDevice(nodeID, key0); err != nil {
		t.Fatal(err)
	}
	if h.srv.SleepStopAtDevice(nodeID, key0) != 0 || h.srv.SleepStopAtDevice(nodeID, key1) != stop1 {
		t.Fatal("canceling one device timer affected the other")
	}

	sid, err := h.srv.StartPlayDevice(context.Background(), nodeID, trackID, key0)
	if err != nil {
		t.Fatal(err)
	}
	setup := waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if setup.setup.ALSADevice != "hw:0,0" || !setup.setup.LockDevice || setup.setup.SessionID != sid {
		t.Fatalf("first device SETUP = %+v", setup.setup)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	vol := waitEvent(t, events, zat.TypeVolume, 3*time.Second)
	if vol.volume.Volume != 25 || vol.volume.SessionID != sid {
		t.Fatalf("persisted volume not applied: %+v", vol.volume)
	}
	waitPlaying(t, h.nodes, nodeID, true)
	idle, err := h.srv.StatusDevice(nodeID, key1)
	if err != nil || idle.State != string(zat.StateIdle) || idle.Playing || idle.Volume != 70 || idle.TrackID != "" || idle.DeviceKey != key1 {
		t.Fatalf("other device inherited active state: %+v %v", idle, err)
	}
	drainEvents(events, 150*time.Millisecond)
	if err := h.srv.SetVolumeDevice(nodeID, key1, 80); err != nil {
		t.Fatal(err)
	}
	expectNoEvent(t, events, zat.TypeVolume, 250*time.Millisecond)
	active, err := h.srv.StatusDevice(nodeID, key0)
	if err != nil || active.Volume != 25 || !active.Playing {
		t.Fatalf("idle volume change affected active device: %+v %v", active, err)
	}
	if idle, err = h.srv.StatusDevice(nodeID, key1); err != nil || idle.Volume != 80 {
		t.Fatalf("idle volume not persisted: %+v %v", idle, err)
	}
	if err := h.srv.SetVolumeDevice(nodeID, key0, 30); err != nil {
		t.Fatal(err)
	}
	vol = waitEvent(t, events, zat.TypeVolume, 3*time.Second)
	if vol.volume.Volume != 30 || vol.volume.SessionID != sid {
		t.Fatalf("active volume frame = %+v", vol.volume)
	}
	if err := h.srv.PauseDevice(nodeID, key1); !errors.Is(err, ErrNoSession) {
		t.Fatalf("pause other device = %v", err)
	}
	if err := h.srv.SeekDevice(nodeID, key1, 1000); !errors.Is(err, ErrNoSession) {
		t.Fatalf("seek other device = %v", err)
	}
	if err := h.srv.StopDevice(nodeID, key1); err != nil {
		t.Fatalf("stop other device: %v", err)
	}
	expectNoEvent(t, events, zat.TypeStop, 250*time.Millisecond)
	waitPlaying(t, h.nodes, nodeID, true)
	if err := h.srv.StopDevice(nodeID, key0); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
	waitPlaying(t, h.nodes, nodeID, false)

	if _, err := h.srv.StartPlayDevice(context.Background(), nodeID, trackID, key1); err != nil {
		t.Fatal(err)
	}
	setup = waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if setup.setup.ALSADevice != "hw:1,0" || !setup.setup.LockDevice {
		t.Fatalf("switched device SETUP = %+v", setup.setup)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	vol = waitEvent(t, events, zat.TypeVolume, 3*time.Second)
	if vol.volume.Volume != 80 {
		t.Fatalf("switched device volume = %+v", vol.volume)
	}
	waitPlaying(t, h.nodes, nodeID, true)
	if err := h.srv.StopDevice(nodeID, key1); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
	waitPlaying(t, h.nodes, nodeID, false)
}

func TestPlaybackDeviceSeekLockAndHotplugStop(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-hotplug-stop", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	key := makeDeviceKey(nodeID, "hw:0,0")
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	updates := make(chan []zat.ALSADevice, 1)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{
		caps: zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0"}}, persistent: true, deviceUpdates: updates,
	}, ready, events)
	<-ready
	if _, err := h.srv.StartPlayDevice(context.Background(), nodeID, trackID, key); err != nil {
		t.Fatal(err)
	}
	setup := waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if !setup.setup.LockDevice || setup.setup.ALSADevice != "hw:0,0" {
		t.Fatalf("initial SETUP = %+v", setup.setup)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	if err := h.srv.PauseDevice(nodeID, key); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypePause, 3*time.Second)
	drainEvents(events, 150*time.Millisecond)
	if err := h.srv.SeekDevice(nodeID, key, 100000); err != nil {
		t.Fatal(err)
	}
	evs := collectEvents(events, 600*time.Millisecond)
	seekSetup, i := findEvent(evs, zat.TypeSetup)
	if i < 0 || !seekSetup.setup.LockDevice || seekSetup.setup.ALSADevice != "hw:0,0" || seekSetup.setup.TotalSamples != 200000 {
		t.Fatalf("seek SETUP lost lock or device: %+v", evs)
	}
	updates <- []zat.ALSADevice{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := h.srv.Status(nodeID)
		if len(st.Devices) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st, _ := h.srv.Status(nodeID); len(st.Devices) != 0 {
		t.Fatalf("unplugged devices still reported: %v", st.Devices)
	}
	if _, err := h.srv.resolveTarget(nodeID, key); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("unplugged target still resolves: %v", err)
	}
	if err := h.srv.StopDevice(nodeID, key); err != nil {
		t.Fatalf("stop after unplug: %v", err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
	waitPlaying(t, h.nodes, nodeID, false)
	if _, err := h.srv.StartPlayDevice(context.Background(), nodeID, trackID, key); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("play on unplugged device = %v", err)
	}
}

func TestPlaybackDeviceStopAfterDeactivation(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-deactivated-stop", true)
	trackID, _ := h.addWAVTrack(t, 200000)
	key := makeDeviceKey(nodeID, "hw:0,0")
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 128)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{
		caps: zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0"}}, persistent: true,
	}, ready, events)
	<-ready
	if _, err := h.srv.StartPlayDevice(context.Background(), nodeID, trackID, key); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.resolveTarget(nodeID, key); !errors.Is(err, ErrDeviceNotActivated) {
		t.Fatalf("deactivated target still resolves: %v", err)
	}
	if err := h.srv.StopDevice(nodeID, key); err != nil {
		t.Fatalf("stop after deactivation: %v", err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
	waitPlaying(t, h.nodes, nodeID, false)
}

func TestPlaybackDeviceAutoAdvanceAndLegacyWrapper(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-auto-device", true)
	track1, _ := h.addWAVTrack(t, 1000)
	track2 := track1
	key0 := makeDeviceKey(nodeID, "hw:0,0")
	key1 := makeDeviceKey(nodeID, "hw:1,0")
	for _, hw := range []string{"hw:0,0", "hw:1,0"} {
		if err := h.nodes.SetDeviceEnabled(nodeID, hw, true); err != nil {
			t.Fatal(err)
		}
	}
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{
		caps: zat.Caps{MaxChannels: 2, ALSADevices: []string{"hw:0,0", "hw:1,0"}}, persistent: true,
	}, ready, events)
	<-ready
	for _, key := range []string{key0, key1} {
		if err := h.srv.SetQueueDevice(nodeID, key, []string{track1, track2}, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := h.srv.SetQueueModeDevice(nodeID, key, QueueModeLoopAll); err != nil {
			t.Fatal(err)
		}
	}
	go h.srv.autoAdvance(key1, track1)
	setup := waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if setup.setup.ALSADevice != "hw:1,0" || !setup.setup.LockDevice || setup.setup.TrackID != track2 {
		t.Fatalf("auto-advance changed device: %+v", setup.setup)
	}
	waitEvent(t, events, zat.TypePlay, 3*time.Second)
	q0, err := h.srv.QueueDevice(nodeID, key0)
	if err != nil || q0.Position != 0 {
		t.Fatalf("other queue advanced: %+v %v", q0, err)
	}
	q1, err := h.srv.QueueDevice(nodeID, key1)
	if err != nil || q1.Position != 1 {
		t.Fatalf("device queue did not advance: %+v %v", q1, err)
	}
	if err := h.srv.StopDevice(nodeID, key1); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
	if err := h.srv.SetDevice(nodeID, "hw:1,0"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.StartPlay(context.Background(), nodeID, track1); err != nil {
		t.Fatal(err)
	}
	setup = waitEvent(t, events, zat.TypeSetup, 3*time.Second)
	if setup.setup.ALSADevice != "hw:1,0" || setup.setup.LockDevice {
		t.Fatalf("legacy wrapper SETUP = %+v", setup.setup)
	}
	if err := h.srv.Stop(nodeID); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, events, zat.TypeStop, 5*time.Second)
}

func TestDSDDefaultsToPCMConversion(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-dsd-pcm", true)
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='hw:0,0' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "DSD"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDSF(t, filepath.Join(target, "DSD", "song.dsf"))
	if _, err := h.lib.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, _ := h.lib.ListTracks(m.ID)
	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 64)
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, DoP: true, ALSADevices: []string{"hw:0,0"}}}, ready, events)
	<-ready
	if _, err := h.srv.StartPlay(context.Background(), nodeID, tracks[0].ID); err != nil {
		t.Fatalf("StartPlay default pcm: %v", err)
	}
	su := waitEvent(t, events, zat.TypeSetup, 5*time.Second)
	if su.setup.DoP {
		t.Fatalf("default DSD must not use DoP: %+v", su.setup)
	}
	if su.setup.Rate != 176400 && su.setup.Rate != 88200 {
		t.Fatalf("converted PCM rate = %d", su.setup.Rate)
	}
	st, err := h.srv.Status(nodeID)
	if err != nil || st.DoP || !st.Converted {
		t.Fatalf("default pcm status = %+v err=%v", st, err)
	}
	_ = h.srv.Stop(nodeID)
}

func TestDSDFallbackToPCM(t *testing.T) {
	h := setup(t)
	nodeID, secret := h.register(t, "ep-dop-pcm", true)
	if _, err := h.st.DB.Exec(`UPDATE nodes SET dac_device='hw:0,0' WHERE node_id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceEnabled(nodeID, "hw:0,0", true); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.SetDeviceDSDMode(nodeID, "hw:0,0", "dop"); err != nil {
		t.Fatal(err)
	}
	m, err := h.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := h.st.DB.Exec(`UPDATE mounts SET target=?,status='mounted' WHERE id=?`, target, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "DSD"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDSF(t, filepath.Join(target, "DSD", "song.dsf"))
	if _, err := h.lib.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, _ := h.lib.ListTracks(m.ID)

	old := openFallbackPCM
	defer func() { openFallbackPCM = old }()
	openFallbackPCM = func(path string, rate, channels, durationMS int) (decode.Decoder, error) {
		return &fakePCMDecoder{format: decode.PCMFormat{Format: zat.S24LE, Rate: rate, Channels: channels}, total: uint64(rate)}, nil
	}

	ready := make(chan struct{}, 1)
	events := make(chan ctrlEvent, 512)
	// DoP capability present, but the ACK never confirms bit-perfect, which is
	// exactly the case that must fall back to ffmpeg PCM.
	go runCtrlEndpointOpts(t, h.srv.Addr(), nodeID, secret, ctrlOpts{caps: zat.Caps{MaxChannels: 2, DoP: true, ALSADevices: []string{"hw:0,0"}, ALSADeviceInfo: []zat.ALSADevice{{HW: "hw:0,0", Name: "Test DAC"}}}, bitPerfect: false}, ready, events)
	<-ready
	deviceKey := makeDeviceKey(nodeID, "hw:0,0")
	if _, err := h.srv.StartPlayDevice(context.Background(), nodeID, tracks[0].ID, deviceKey); err != nil {
		t.Fatalf("StartPlay fallback: %v", err)
	}
	evs := collectEvents(events, 500*time.Millisecond)
	var setups []zat.Setup
	for _, ev := range evs {
		if ev.typ == zat.TypeSetup {
			setups = append(setups, ev.setup)
		}
	}
	if len(setups) != 2 || !setups[0].DoP || setups[1].DoP || setups[1].Rate != 176400 || setups[1].Format != zat.S24LE {
		t.Fatalf("setups = %+v", setups)
	}
	for _, su := range setups {
		if !su.LockDevice || su.ALSADevice != "hw:0,0" {
			t.Fatalf("fallback SETUP lost device lock: %+v", su)
		}
	}
	st, err := h.srv.Status(nodeID)
	if err != nil || !st.Converted || st.DoP || st.DSDRate != 2822400 {
		t.Fatalf("fallback status = %+v err=%v", st, err)
	}
	if err := h.srv.SetVolumeDevice(nodeID, deviceKey, 40); err != nil {
		t.Fatalf("volume after PCM fallback should work: %v", err)
	}
	if err := h.srv.StopDevice(nodeID, deviceKey); err != nil {
		t.Fatal(err)
	}
}
