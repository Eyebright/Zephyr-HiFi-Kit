package zatclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"hifihub/agent/internal/player"
	"hifihub/zat"
)

// --- fake player ---

type testPlayer struct {
	mu       sync.Mutex
	format   zat.Format
	rate     int
	channels int
	device   string
	pcm      []byte
	writes   int
	drained  bool
	closed   bool
	openErr  error
	writeErr error
	bitPerf  bool
}

func (p *testPlayer) Open(f zat.Format, rate, ch int, dev string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.openErr != nil {
		return p.openErr
	}
	p.format, p.rate, p.channels, p.device = f, rate, ch, dev
	return nil
}
func (p *testPlayer) OpenBitPerfect(f zat.Format, rate, ch int, dev string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.openErr != nil {
		return p.openErr
	}
	p.format, p.rate, p.channels, p.device = f, rate, ch, dev
	p.bitPerf = true
	return nil
}

func (p *testPlayer) BitPerfect() (bool, string) { return p.bitPerf, "test" }

func (p *testPlayer) Write(pcm []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeErr != nil {
		return p.writeErr
	}
	p.pcm = append(p.pcm, pcm...)
	p.writes++
	return nil
}
func (p *testPlayer) Drain() error { p.mu.Lock(); p.drained = true; p.mu.Unlock(); return nil }
func (p *testPlayer) Drop() error  { p.mu.Lock(); p.drained = true; p.mu.Unlock(); return nil }
func (p *testPlayer) Close() error { p.mu.Lock(); p.closed = true; p.mu.Unlock(); return nil }
func (p *testPlayer) snapshot() ([]byte, int, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.pcm...), p.writes, p.drained, p.closed
}

func rampPCM(frames, channels, bps int) []byte {
	pcm := make([]byte, frames*channels*bps)
	for i := 0; i < frames*channels; i++ {
		v := int32((i*137)%65536) - 32768
		off := i * bps
		for k := 0; k < bps; k++ {
			pcm[off+k] = byte(v >> (8 * k))
		}
	}
	return pcm
}

// --- fake source (server side of one connection) ---

type fakeSource struct {
	t    *testing.T
	ln   net.Listener
	conn net.Conn
	seq  uint32
}

func newFakeSource(t *testing.T) *fakeSource {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &fakeSource{t: t, ln: ln}
}
func (f *fakeSource) addr() string { return f.ln.Addr().String() }
func (f *fakeSource) accept() {
	f.t.Helper()
	_ = f.ln.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	c, err := f.ln.Accept()
	if err != nil {
		f.t.Fatalf("accept: %v", err)
	}
	f.conn = c
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
}
func (f *fakeSource) read() zat.Frame {
	f.t.Helper()
	fr, err := zat.ReadFrame(f.conn)
	if err != nil {
		f.t.Fatalf("source read: %v", err)
	}
	return fr
}
func (f *fakeSource) send(ty zat.Type, v any) {
	f.t.Helper()
	f.seq++
	fr, err := zat.ControlFrame(ty, f.seq, v)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := zat.WriteFrame(f.conn, fr); err != nil {
		f.t.Fatalf("source send %s: %v", ty, err)
	}
}
func (f *fakeSource) sendAudio(pts uint64, pcm []byte) {
	f.t.Helper()
	f.seq++
	fr := zat.AudioFrame(f.seq, zat.AudioPayload{PTS: pts, TrackID: "trk-1", PCM: pcm})
	if err := zat.WriteFrame(f.conn, fr); err != nil {
		f.t.Fatalf("source audio: %v", err)
	}
}
func (f *fakeSource) close() {
	_ = f.ln.Close()
	if f.conn != nil {
		_ = f.conn.Close()
	}
}

func newTestClient(addr string, fp *testPlayer) *Client {
	c := New(addr, "test/0.1", func() (string, string) { return "n-1", "secret-1" },
		func() []zat.ALSADevice { return []zat.ALSADevice{{HW: "hw:0,0", Name: "DAC"}, {HW: "null"}} })
	c.WithPlayerFactory(func() player.Player { return fp })
	return c
}

func runClient(c *Client) chan error {
	done := make(chan error, 1)
	go func() { done <- c.connectAndServe(context.Background(), "n-1", "secret-1") }()
	return done
}

// --- tests ---

func TestApplyVolume(t *testing.T) {
	pcm := rampPCM(4, 2, 2)
	if got := applyVolume(pcm, zat.S16LE, 100); !bytes.Equal(got, pcm) {
		t.Fatal("unity volume must return input unchanged")
	}
	if got := applyVolume(pcm, zat.S16LE, 0); !bytes.Equal(got, make([]byte, len(pcm))) {
		t.Fatal("zero volume must be silence")
	}
	half := applyVolume(pcm, zat.S16LE, 50)
	for i := 0; i+2 <= len(pcm); i += 2 {
		src := int16(binary.LittleEndian.Uint16(pcm[i:]))
		got := int16(binary.LittleEndian.Uint16(half[i:]))
		if int(got) != int(src)/2 {
			t.Fatalf("S16 sample %d: got %d want %d", i/2, got, int(src)/2)
		}
	}
	// S24 and S32 scale without overflow.
	for _, f := range []zat.Format{zat.S24LE, zat.S32LE} {
		p := rampPCM(4, 2, f.BytesPerSample())
		out := applyVolume(p, f, 50)
		if len(out) != len(p) {
			t.Fatalf("%s length mismatch", f)
		}
	}
}

func TestSetDeviceFromSource(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)

	src.accept()
	if fr := src.read(); fr.Type != zat.TypeHello {
		t.Fatalf("expected HELLO, got %s", fr.Type)
	}
	src.send(zat.TypeHelloAck, zat.HelloAck{ServerVersion: "zat/0.1", PrebufferMS: 200})
	src.send(zat.TypeSetDevice, zat.SetDevice{Device: "hw:1,0"})

	deadline := time.Now().Add(3 * time.Second)
	for c.PreferredDevice() != "hw:1,0" {
		if time.Now().After(deadline) {
			t.Fatal("SET_DEVICE did not set the preferred device")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Idle STATUS (no session yet) must carry the preferred device.
	var sawPref bool
	for time.Now().Before(deadline) && !sawPref {
		_ = src.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			break
		}
		if fr.Type == zat.TypeStatus {
			var st zat.Status
			_ = json.Unmarshal(fr.Payload, &st)
			if st.PrefDevice == "hw:1,0" && st.SessionID == "" {
				sawPref = true
			}
		}
	}
	if !sawPref {
		t.Fatal("idle STATUS never reported pref_device")
	}

	// The preferred device overrides the SETUP suggestion.
	deadline = time.Now().Add(3 * time.Second)
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "hw:0,0"})
	var ack zat.SetupAck
	for time.Now().Before(deadline) {
		_ = src.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			t.Fatalf("setup ack: %v", err)
		}
		if fr.Type == zat.TypeSetupAck {
			_ = json.Unmarshal(fr.Payload, &ack)
			break
		}
	}
	if !ack.OK {
		t.Fatalf("setup ack not ok: %+v", ack)
	}
	fp.mu.Lock()
	dev := fp.device
	fp.mu.Unlock()
	if dev != "hw:1,0" {
		t.Fatalf("player opened %q, want preferred hw:1,0", dev)
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not finish")
	}
}

func TestHandshakePlaybackRoundTrip(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)

	src.accept()
	hello := src.read()
	if hello.Type != zat.TypeHello {
		t.Fatalf("expected HELLO, got %s", hello.Type)
	}
	var h zat.Hello
	_ = json.Unmarshal(hello.Payload, &h)
	if h.NodeID != "n-1" || h.SecretSHA256 != secretHash("secret-1") {
		t.Fatalf("bad hello creds: %+v", h)
	}
	if len(h.Caps.ALSADevices) != 2 {
		t.Fatalf("caps devices not declared: %+v", h.Caps)
	}
	src.send(zat.TypeHelloAck, zat.HelloAck{SessionID: "", ServerVersion: "zat/0.1", PrebufferMS: 200})

	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "hw:0,0", TotalSamples: 3000})
	ackF := src.read()
	if ackF.Type != zat.TypeSetupAck {
		t.Fatalf("expected SETUP_ACK, got %s", ackF.Type)
	}
	var ack zat.SetupAck
	_ = json.Unmarshal(ackF.Payload, &ack)
	if !ack.OK || ack.Opened.Rate != 44100 || ack.Opened.Format != zat.S16LE {
		t.Fatalf("bad setup ack: %+v", ack)
	}

	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	src.send(zat.TypeTrackMeta, zat.TrackMeta{TrackID: "trk-1", Title: "One More Time", Artist: "Daft Punk", DurationMS: 5000, Codec: "wav", Rate: 44100, Channels: 2})
	pcm := rampPCM(3000, 2, 2)
	// Send in two chunks to exercise multi-frame rendering.
	half := len(pcm) / 2
	half -= half % 4
	src.sendAudio(0, pcm[:half])
	src.sendAudio(uint64(half/4), pcm[half:])

	// Collect a STATUS while playing.
	deadline := time.Now().Add(3 * time.Second)
	var sawPlaying bool
	var lastPTS uint64
	for time.Now().Before(deadline) {
		_ = src.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			break
		}
		if fr.Type == zat.TypeStatus {
			var st zat.Status
			_ = json.Unmarshal(fr.Payload, &st)
			if st.State == "playing" {
				sawPlaying = true
				lastPTS = st.PTS
			}
		}
	}
	if !sawPlaying {
		t.Fatal("never received a playing STATUS")
	}
	if !c.Playing() {
		t.Fatal("client.Playing() false during playback")
	}
	snap := c.Snapshot()
	if snap.Title != "One More Time" || snap.Artist != "Daft Punk" {
		t.Fatalf("meta not captured: %+v", snap)
	}

	_ = src.conn.SetDeadline(time.Now().Add(5 * time.Second))
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not finish after STOP")
	}
	got, writes, drained, _ := fp.snapshot()
	if !bytes.Equal(got, pcm) {
		t.Fatalf("rendered PCM != source (%d vs %d bytes, %d writes)", len(got), len(pcm), writes)
	}
	if !drained {
		t.Fatal("player not drained on STOP")
	}
	if lastPTS == 0 {
		t.Fatal("STATUS pts never advanced")
	}
	if c.Playing() {
		t.Fatal("client.Playing() true after STOP")
	}
}

func TestAuthRejected(t *testing.T) {
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), &testPlayer{})
	done := runClient(c)
	src.accept()
	src.read() // HELLO
	src.send(zat.TypeError, zat.Error{Code: zat.ErrAuthFailed, Detail: "secret mismatch", Fatal: true})
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected auth error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client hung on auth rejection")
	}
	if c.Playing() {
		t.Fatal("playing after auth failure")
	}
}

func TestSetupOpenFailure(t *testing.T) {
	fp := &testPlayer{openErr: errors.New("alsa open failed")}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "hw:9,9"})
	ackF := src.read()
	var ack zat.SetupAck
	_ = json.Unmarshal(ackF.Payload, &ack)
	if ack.OK {
		t.Fatal("SETUP_ACK ok=true despite open failure")
	}
	if ack.Detail == "" {
		t.Fatal("SETUP_ACK missing failure detail")
	}
	src.send(zat.TypeTeardown, zat.Teardown{SessionID: "s-1", Reason: "setup_failed"})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung after setup failure")
	}
}

func TestLocalStopSendsTeardown(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "default"})
	src.read() // SETUP_ACK
	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	src.sendAudio(0, rampPCM(1000, 2, 2))

	// Wait until the client reports playing, then stop locally.
	waitPlaying(t, c, true)
	if err := c.Stop(); err != nil {
		t.Fatalf("local stop: %v", err)
	}
	// The source must receive TEARDOWN.
	_ = src.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var sawTeardown bool
	for {
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			break
		}
		if fr.Type == zat.TypeTeardown {
			var td zat.Teardown
			_ = json.Unmarshal(fr.Payload, &td)
			if td.Reason == "user_stop" {
				sawTeardown = true
				break
			}
		}
	}
	if !sawTeardown {
		t.Fatal("local stop did not send TEARDOWN(user_stop)")
	}
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not finish after local stop")
	}
	if c.Playing() {
		t.Fatal("playing after local stop")
	}
}

func TestPauseDiscardsAudioThenResume(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "default"})
	src.read()
	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	waitPlaying(t, c, true)

	paused, err := c.TogglePause()
	if err != nil || !paused {
		t.Fatalf("toggle pause: paused=%v err=%v", paused, err)
	}
	if c.Playing() {
		t.Fatal("Playing() true while paused")
	}
	// Audio sent while paused must be discarded.
	src.sendAudio(0, rampPCM(500, 2, 2))
	time.Sleep(150 * time.Millisecond)
	if _, writes, _, _ := fp.snapshot(); writes != 0 {
		t.Fatalf("audio rendered while paused: %d writes", writes)
	}
	// Resume.
	paused, _ = c.TogglePause()
	if paused {
		t.Fatal("expected resume")
	}
	waitPlaying(t, c, true)
	src.sendAudio(500, rampPCM(500, 2, 2))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, writes, _, _ := fp.snapshot(); writes > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, writes, _, _ := fp.snapshot(); writes == 0 {
		t.Fatal("no audio rendered after resume")
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung")
	}
}

// gatePlayer blocks the first Write until released, emulating the ALSA sample
// clock so a pause can land mid-render (the real-device resume scenario).
type gatePlayer struct {
	*testPlayer
	gate chan struct{}
}

func (g *gatePlayer) Write(pcm []byte) error {
	<-g.gate
	return g.testPlayer.Write(pcm)
}

// TestPauseHoldsQueuedAudioThenResume pauses while a render is blocked and more
// chunks are queued: the held chunks must survive the pause and render on
// resume (a nil-channel pause gate without a resume wake hangs here).
func TestPauseHoldsQueuedAudioThenResume(t *testing.T) {
	fp := &testPlayer{}
	gp := &gatePlayer{testPlayer: fp, gate: make(chan struct{})}
	src := newFakeSource(t)
	defer src.close()
	c := New(src.addr(), "test/0.1", func() (string, string) { return "n-1", "secret-1" },
		func() []zat.ALSADevice { return []zat.ALSADevice{{HW: "null"}} })
	c.WithPlayerFactory(func() player.Player { return gp })
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "null"})
	src.read()
	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	waitPlaying(t, c, true)

	c1 := rampPCM(500, 2, 2)
	c2 := rampPCM(500, 2, 2)
	c3 := rampPCM(500, 2, 2)
	src.sendAudio(0, c1)
	time.Sleep(80 * time.Millisecond) // renderLoop is now blocked inside Write
	src.sendAudio(500, c2)
	src.sendAudio(1000, c3)
	src.send(zat.TypePause, zat.Pause{SessionID: "s-1"})
	waitPlaying(t, c, false)
	close(gp.gate) // finish the in-flight write; loop must now hold c2/c3
	time.Sleep(150 * time.Millisecond)
	if _, writes, _, _ := fp.snapshot(); writes != 1 {
		t.Fatalf("held chunks rendered while paused: writes=%d want 1", writes)
	}

	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	waitPlaying(t, c, true)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, writes, _, _ := fp.snapshot(); writes >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	pcm, writes, _, _ := fp.snapshot()
	if writes != 3 {
		t.Fatalf("resume did not render held chunks: writes=%d want 3", writes)
	}
	want := append(append(append([]byte{}, c1...), c2...), c3...)
	if !bytes.Equal(pcm, want) {
		t.Fatal("resume PCM not continuous with pre-pause audio")
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung")
	}
}

func TestSeekSkipsForward(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "default", TotalSamples: 2000})
	src.read()
	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	waitPlaying(t, c, true)
	// Seek to sample 1000; the first 1000 samples must be skipped.
	if err := c.Seek(1000); err != nil {
		t.Fatal(err)
	}
	pcm := rampPCM(2000, 2, 2)
	src.sendAudio(0, pcm[:1000*4])    // entirely before target -> discarded
	src.sendAudio(1000, pcm[1000*4:]) // from target -> rendered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _, _, _ := fp.snapshot(); len(got) >= 1000*4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _, _, _ := fp.snapshot()
	if !bytes.Equal(got, pcm[1000*4:]) {
		t.Fatalf("seek rendered wrong range: got %d bytes want %d", len(got), len(pcm)-1000*4)
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung")
	}
}

func TestVolumeAppliedToRender(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-1", TrackID: "trk-1", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "default"})
	src.read()
	src.send(zat.TypePlay, zat.Play{SessionID: "s-1"})
	waitPlaying(t, c, true)
	if err := c.SetVolume(50); err != nil {
		t.Fatal(err)
	}
	pcm := rampPCM(500, 2, 2)
	src.sendAudio(0, pcm)
	want := applyVolume(pcm, zat.S16LE, 50)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _, _, _ := fp.snapshot(); len(got) >= len(want) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _, _, _ := fp.snapshot()
	if !bytes.Equal(got, want) {
		t.Fatal("rendered PCM not volume-scaled")
	}
	if c.Snapshot().Volume != 50 {
		t.Fatalf("snapshot volume = %d", c.Snapshot().Volume)
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-1"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung")
	}
}

func TestPingPong(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)
	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{PrebufferMS: 200})
	src.send(zat.TypePing, zat.Ping{Nonce: "abc123"})
	_ = src.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var sawPong bool
	for {
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			break
		}
		if fr.Type == zat.TypePong {
			var p zat.Pong
			_ = json.Unmarshal(fr.Payload, &p)
			if p.Nonce == "abc123" {
				sawPong = true
				break
			}
		}
	}
	if !sawPong {
		t.Fatal("no PONG with matching nonce")
	}
	src.send(zat.TypeTeardown, zat.Teardown{Reason: "bye"})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client hung")
	}
}

func waitPlaying(t *testing.T, c *Client, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Playing() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Playing() never became %v", want)
}

func TestMQASetupLocksVolume(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)

	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{SessionID: "", ServerVersion: "zat/0.1", PrebufferMS: 0})

	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-mqa", TrackID: "trk-mqa", Format: zat.S16LE, Rate: 44100, Channels: 2, ALSADevice: "hw:0,0", TotalSamples: 1000, MQA: true})
	var ack zat.SetupAck
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = src.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			t.Fatalf("setup ack: %v", err)
		}
		if fr.Type == zat.TypeSetupAck {
			_ = json.Unmarshal(fr.Payload, &ack)
			break
		}
	}
	if !ack.OK || !ack.BitPerfect {
		t.Fatalf("MQA setup ack = %+v", ack)
	}
	fp.mu.Lock()
	if !fp.bitPerf {
		t.Fatal("player did not open the bit-perfect path")
	}
	fp.mu.Unlock()

	src.send(zat.TypePlay, zat.Play{SessionID: "s-mqa"})
	src.send(zat.TypeVolume, zat.Volume{SessionID: "s-mqa", Volume: 20})
	pcm := rampPCM(1000, 2, 2)
	src.sendAudio(0, pcm)

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, writes, _, _ := fp.snapshot(); writes > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if vol := c.Snapshot().Volume; vol != 100 {
		t.Fatalf("MQA session volume = %d want 100", vol)
	}
	got, _, _, _ := fp.snapshot()
	if !bytes.Equal(got, pcm) {
		t.Fatalf("MQA PCM was altered: got %d bytes, want %d", len(got), len(pcm))
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-mqa"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not finish")
	}
}

func TestDoPSetupLocksVolume(t *testing.T) {
	fp := &testPlayer{}
	src := newFakeSource(t)
	defer src.close()
	c := newTestClient(src.addr(), fp)
	done := runClient(c)

	src.accept()
	src.read()
	src.send(zat.TypeHelloAck, zat.HelloAck{SessionID: "", ServerVersion: "zat/0.1", PrebufferMS: 0})

	src.send(zat.TypeSetup, zat.Setup{SessionID: "s-dop", TrackID: "trk-dop", Format: zat.S24LE, Rate: 176400, Channels: 2, ALSADevice: "hw:0,0", TotalSamples: 1000, DoP: true, DSDRate: 2822400})
	var ack zat.SetupAck
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = src.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		fr, err := zat.ReadFrame(src.conn)
		if err != nil {
			t.Fatalf("setup ack: %v", err)
		}
		if fr.Type == zat.TypeSetupAck {
			_ = json.Unmarshal(fr.Payload, &ack)
			break
		}
	}
	if !ack.OK || !ack.BitPerfect {
		t.Fatalf("DoP setup ack = %+v", ack)
	}
	fp.mu.Lock()
	if !fp.bitPerf {
		t.Fatal("player did not open the bit-perfect path")
	}
	fp.mu.Unlock()
	if snap := c.Snapshot(); !snap.DoP || snap.DSDRate != 2822400 {
		t.Fatalf("snapshot = %+v", snap)
	}

	src.send(zat.TypePlay, zat.Play{SessionID: "s-dop"})
	src.send(zat.TypeVolume, zat.Volume{SessionID: "s-dop", Volume: 20})
	pcm := rampPCM(1000*6, 1, 1) // six bytes per DoP frame; opaque bytes
	src.sendAudio(0, pcm)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, writes, _, _ := fp.snapshot(); writes > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if vol := c.Snapshot().Volume; vol != 100 {
		t.Fatalf("DoP session volume = %d want 100", vol)
	}
	got, _, _, _ := fp.snapshot()
	if !bytes.Equal(got, pcm) {
		t.Fatalf("DoP PCM was altered: got %d bytes, want %d", len(got), len(pcm))
	}
	src.send(zat.TypeStop, zat.Stop{SessionID: "s-dop"})
	src.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not finish")
	}
}

func TestDeviceReportTracksHotplug(t *testing.T) {
	old := cardsSignature
	defer func() { cardsSignature = old }()
	sig := "cards-v1"
	cardsSignature = func() string { return sig }
	devs := []zat.ALSADevice{{HW: "hw:0,0", Name: "b1"}}
	c := New("127.0.0.1:0", "test", func() (string, string) { return "", "" }, func() []zat.ALSADevice { return devs })
	s := newSession(c, nil, "n")
	now := time.Now()
	hws, info := s.deviceReport(now)
	if len(hws) != 1 || hws[0] != "hw:0,0" || len(info) != 1 || info[0].Name != "b1" {
		t.Fatalf("first report wrong: %v %v", hws, info)
	}
	if hws, _ := s.deviceReport(now.Add(time.Second)); hws != nil {
		t.Fatalf("unchanged card list must not re-report: %v", hws)
	}
	sig = "cards-v2"
	devs = append(devs, zat.ALSADevice{HW: "hw:3,0", Name: "AUDIO"})
	hws, info = s.deviceReport(now.Add(2 * time.Second))
	if len(hws) != 2 || hws[1] != "hw:3,0" || len(info) != 2 || info[1].Name != "AUDIO" {
		t.Fatalf("hotplug not reported: %v %v", hws, info)
	}

	// Without procfs the signature stays empty; fall back to periodic refresh.
	sig = ""
	s2 := newSession(c, nil, "n2")
	if hws, _ := s2.deviceReport(now); len(hws) != 2 {
		t.Fatalf("first report without procfs: %v", hws)
	}
	if hws, _ := s2.deviceReport(now.Add(10 * time.Second)); hws != nil {
		t.Fatalf("fallback refreshed too early: %v", hws)
	}
	if hws, _ := s2.deviceReport(now.Add(devRefreshEvery + time.Second)); len(hws) != 2 {
		t.Fatalf("fallback refresh missing: %v", hws)
	}
}
