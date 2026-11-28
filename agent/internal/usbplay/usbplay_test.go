package usbplay

import (
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"hifihub/agent/internal/usbfs"
)

func stereo48(sub int) usbfs.Geometry {
	return usbfs.Geometry{Rate: 48000, Channels: 2, Subslot: sub, FramesPerSecond: 8000}
}

// decode reads one sample back out of its subslot, little-endian.
func decode(sub int, b []byte) int32 {
	switch sub {
	case 2:
		return int32(int16(uint16(b[0]) | uint16(b[1])<<8))
	case 3:
		v := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
		if v&0x800000 != 0 {
			v |= 0xff000000
		}
		return int32(v)
	default:
		return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	}
}

func TestToneEncodesWholeSamples(t *testing.T) {
	g := stereo48(2)
	tone, err := NewTone(g, 440, 880)
	if err != nil {
		t.Fatalf("NewTone: %v", err)
	}
	tone.Amplitude(1)
	dst := make([]byte, 4*g.SampleBytes())
	if err := tone.Fill(dst); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if tone.Samples() != 4 {
		t.Fatalf("samples = %d, want 4", tone.Samples())
	}
	// The first frame sits at phase zero, so both channels start silent.
	for ch := 0; ch < 2; ch++ {
		if v := decode(2, dst[ch*2:]); v != 0 {
			t.Errorf("channel %d starts at %d, want 0", ch, v)
		}
	}
	// Half a period of 440 Hz at 48 kHz is 54.5 sample frames, so a quarter
	// period away the left channel must be near full scale.
	quarter := int(math.Round(float64(g.Rate) / 4 / 440))
	tone2, _ := NewTone(g, 440, 880)
	tone2.Amplitude(1)
	buf := make([]byte, (quarter+1)*g.SampleBytes())
	if err := tone2.Fill(buf); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	got := decode(2, buf[quarter*g.SampleBytes():])
	if got < 30000 {
		t.Errorf("sample %d of a 440 Hz tone = %d, want near full scale", quarter, got)
	}
	// A 100 ms run must advance the phase by exactly 4800 sample frames.
	if err := tone.Fill(make([]byte, 96*g.SampleBytes())); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if tone.Samples() != 100 {
		t.Errorf("samples = %d, want 100", tone.Samples())
	}
}

func TestToneKeepsAQuarterPeriodOnPhase(t *testing.T) {
	g := stereo48(2)
	tone, _ := NewTone(g, 1000)
	tone.Amplitude(1)
	buf := make([]byte, 48*g.SampleBytes())
	if err := tone.Fill(buf); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	peak := 0
	for i := 0; i < 48; i++ {
		if v := int(decode(2, buf[i*g.SampleBytes():])); v > peak {
			peak = v
		}
	}
	if peak < 32000 {
		t.Errorf("1 kHz tone over one millisecond peaked at %d, want full scale", peak)
	}
}

// The level must not depend on the width the DAC was negotiated at, or a
// 32-bit device would play 48 dB quieter than a 16-bit one.
func TestToneLevelFollowsTheSubslot(t *testing.T) {
	want := map[int]float64{2: 32768, 3: 8388608, 4: 2147483648}
	for sub, full := range want {
		g := stereo48(sub)
		tone, err := NewTone(g, 1000)
		if err != nil {
			t.Fatalf("subslot %d: %v", sub, err)
		}
		tone.Amplitude(0.5)
		buf := make([]byte, 480*g.SampleBytes())
		if err := tone.Fill(buf); err != nil {
			t.Fatalf("subslot %d: Fill: %v", sub, err)
		}
		peak := 0.0
		for i := 0; i < 480; i++ {
			v := math.Abs(float64(decode(sub, buf[i*g.SampleBytes():])))
			if v > peak {
				peak = v
			}
		}
		if math.Abs(peak-full*0.5) > full*0.01 {
			t.Errorf("subslot %d peaked at %.0f, want %.0f (half of %.0f)", sub, peak, full*0.5, full)
		}
	}
}

func TestToneRejectsImpossibleFormats(t *testing.T) {
	if _, err := NewTone(stereo48(5), 440); err == nil {
		t.Error("a 5 byte subslot was accepted")
	}
	if _, err := NewTone(stereo48(2), 440, 880, 1320); err == nil {
		t.Error("three tones for two channels were accepted")
	}
	if _, err := NewTone(stereo48(2), 30000); err == nil {
		t.Error("a tone past Nyquist was accepted")
	}
	bad := stereo48(2)
	bad.Rate = 0
	if _, err := NewTone(bad, 440); err == nil {
		t.Error("an unvalidated geometry was accepted")
	}
}

func TestFillNeedsWholeSamples(t *testing.T) {
	tone, _ := NewTone(stereo48(4), 440)
	if err := tone.Fill(make([]byte, 11)); err == nil {
		t.Fatal("11 bytes is not a whole stereo 32-bit sample")
	}
	if tone.Samples() != 0 {
		t.Errorf("a rejected buffer still advanced the phase to %d", tone.Samples())
	}
}

// fakeTransport stands in for a DAC: it consumes the pump's schedule and, in
// the slow case, takes longer than the audio it moved.
type fakeTransport struct {
	mu       sync.Mutex
	pause    time.Duration // wall time spent per packet, past the audio it covers
	abandons int
	err      error
	seen     []int // payload bytes of each packet, to check the schedule
}

func (f *fakeTransport) Pump(ep byte, g usbfs.Geometry, packets, depth int, src usbfs.Source, stop <-chan struct{}) (*usbfs.Stats, error) {
	stats := &usbfs.Stats{}
	frame := 0
	for {
		select {
		case <-stop:
			return stats, f.err
		default:
		}
		total, sizes := g.URBBytes(frame, packets)
		buf := make([]byte, total)
		n, err := src(buf)
		if err == io.EOF {
			return stats, nil
		}
		if err != nil {
			return stats, err
		}
		if n != total {
			return stats, io.ErrShortWrite
		}
		stats.Submitted++
		if f.pause > 0 {
			time.Sleep(f.pause * time.Duration(packets))
		}
		f.mu.Lock()
		f.seen = append(f.seen, total)
		f.mu.Unlock()
		stats.Completed++
		stats.Bytes += n
		stats.Frames += len(sizes)
		frame += packets
	}
}

func (f *fakeTransport) Abandon() {
	f.mu.Lock()
	f.abandons++
	f.mu.Unlock()
}

func TestRunFinishesItsDuration(t *testing.T) {
	g := stereo48(2)
	f := &fakeTransport{}
	r, err := Start(RunConfig{Transport: f, Endpoint: 0x01, Geometry: g, Duration: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-r.done
	if !r.Done() {
		t.Fatal("the run did not finish")
	}
	rep := r.Report()
	if rep.AudioMS != 20 {
		t.Errorf("audio = %d ms, want 20", rep.AudioMS)
	}
	if rep.Packets != 160 {
		t.Errorf("packets = %d, want 160 microframes", rep.Packets)
	}
	if want := 48 * 20; r.Samples() != want {
		t.Errorf("samples = %d, want %d", r.Samples(), want)
	}
	if rep.Bytes != 20*48*4 {
		t.Errorf("bytes = %d, want %d", rep.Bytes, 20*48*4)
	}
	// The schedule the fake saw must be the pump's own: one millisecond of
	// 48 kHz stereo 16-bit is 192 bytes.
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) != 20 || f.seen[0] != 192 {
		t.Errorf("packet sizes = %d x %v, want 20 of 192 B", len(f.seen), f.seen)
	}
}

func TestRunDefaultsToTheMillisecondGrid(t *testing.T) {
	g := stereo48(4)
	f := &fakeTransport{}
	r, err := Start(RunConfig{Transport: f, Endpoint: 0x01, Geometry: g, Duration: time.Millisecond})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-r.done
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) != 1 || f.seen[0] != 384 {
		t.Errorf("packets = %v, want one of 384 B", f.seen)
	}
}

// A pump that runs behind its own clock is exactly what the screen has to show,
// and it shows up here as negative drift rather than an error code.
func TestReportMeasuresClockError(t *testing.T) {
	g := usbfs.Geometry{Rate: 48000, Channels: 2, Subslot: 2, FramesPerSecond: 8000}
	f := &fakeTransport{pause: 250 * time.Microsecond} // 2 ms wall per 1 ms audio
	r, err := Start(RunConfig{Transport: f, Endpoint: 0x01, Geometry: g, Duration: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-r.done
	rep := r.Report()
	if rep.AudioMS != 10 {
		t.Fatalf("audio = %d ms, want 10", rep.AudioMS)
	}
	if rep.DriftPPM > -300000 {
		t.Errorf("drift = %.0f ppm, want the pump to read as far behind its clock", rep.DriftPPM)
	}
	if rep.ElapsedMS < 18 {
		t.Errorf("elapsed = %d ms, want the stalled pump to have taken its time", rep.ElapsedMS)
	}
}

func TestStopCancelsAnOpenEndedRun(t *testing.T) {
	g := stereo48(2)
	f := &fakeTransport{}
	r, err := Start(RunConfig{Transport: f, Endpoint: 0x01, Geometry: g})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rep := r.Report()
	if rep.Running {
		t.Error("the report still says the run is live")
	}
	if rep.Packets == 0 {
		t.Error("nothing was pumped before the stop")
	}
	// A second Stop must not close the cancel channel twice.
	if err := r.Stop(); err != nil {
		t.Errorf("Stop twice: %v", err)
	}
}

func TestRunReportsTransportFailure(t *testing.T) {
	g := stereo48(2)
	want := io.ErrUnexpectedEOF
	f := &fakeTransport{err: want}
	r, err := Start(RunConfig{Transport: f, Endpoint: 0x01, Geometry: g})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := r.Stop(); err != want {
		t.Errorf("Stop = %v, want the transport error", err)
	}
	if rep := r.Report(); rep.Error != want.Error() {
		t.Errorf("report error = %q, want %q", rep.Error, want.Error())
	}
}

func TestStartRejectsBadConfig(t *testing.T) {
	if _, err := Start(RunConfig{Endpoint: 0x01, Geometry: stereo48(2)}); err != ErrNoTransport {
		t.Errorf("Start without a transport = %v, want ErrNoTransport", err)
	}
	if _, err := Start(RunConfig{Transport: &fakeTransport{}, Endpoint: 0x01}); err == nil {
		t.Error("Start with an unset geometry was accepted")
	}
}
