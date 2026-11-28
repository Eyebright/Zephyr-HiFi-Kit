package usbplay

import (
	"errors"
	"sync"
	"time"

	"hifihub/agent/internal/usbfs"
)

// RunConfig is what the exclusive-output self test hands to the pump. Zero
// Packets and Depth mean "one millisecond each", which is the grid the DAC
// expects and the least the kernel will let us ask for.
type RunConfig struct {
	Transport Transport
	Endpoint  byte
	Geometry  usbfs.Geometry
	Packets   int
	Depth     int
	Duration  time.Duration // zero plays until Stop
	Tones     []float64     // Hz per channel, one value repeats; default 440
	Amplitude float64       // 0..1 of full scale, default 0.25
}

// Report is the verdict the screen shows: how much audio reached the device,
// how long the run took, and whether the two agree.
type Report struct {
	Running   bool    `json:"running"`
	ElapsedMS int64   `json:"elapsedMs"`
	AudioMS   int64   `json:"audioMs"`
	Bytes     int     `json:"bytes"`
	Packets   int     `json:"packets"`
	Submitted int     `json:"submitted"`
	Completed int     `json:"completed"`
	Failures  int     `json:"failures"`
	Short     int     `json:"shortPackets"`
	DriftPPM  float64 `json:"driftPpm"`
	Error     string  `json:"error,omitempty"`
}

// Runner owns one prototype run. Start returns while the pump is still moving
// audio so the UI can poll it, and Stop makes the cancel visible to the kernel
// before reporting.
type Runner struct {
	cfg    RunConfig
	tone   *Tone
	cancel chan struct{}
	done   chan struct{}

	mu      sync.Mutex
	stats   *usbfs.Stats
	err     error
	started time.Time
	ended   time.Time
	live    bool
}

// ErrNoTransport stands for a self test launched without a claimed device.
var ErrNoTransport = errors.New("usbplay: no transport")

// Start validates the run and launches the pump.
func Start(cfg RunConfig) (*Runner, error) {
	if cfg.Transport == nil {
		return nil, ErrNoTransport
	}
	// Deriving the ring from the geometry divides by its frame grid, so the
	// format has to be checked before anything reads it.
	if err := cfg.Geometry.Validate(); err != nil {
		return nil, err
	}
	packets := cfg.Packets
	if packets <= 0 {
		packets = cfg.Geometry.PacketsPerURB()
	}
	depth := cfg.Depth
	if depth <= 0 {
		depth = cfg.Geometry.DepthFor(1)
	}
	if depth > 256 {
		depth = 256 // the kernel caps usbfs memory per descriptor; keep the ring sane
	}
	hz := cfg.Tones
	if len(hz) == 0 {
		hz = []float64{440}
	}
	tone, err := NewTone(cfg.Geometry, hz...)
	if err != nil {
		return nil, err
	}
	if cfg.Amplitude > 0 {
		tone.Amplitude(cfg.Amplitude)
	}
	cfg.Packets, cfg.Depth, cfg.Tones = packets, depth, hz
	r := &Runner{
		cfg:     cfg,
		tone:    tone,
		cancel:  make(chan struct{}),
		done:    make(chan struct{}),
		started: time.Now(),
		live:    true,
	}
	go r.pump()
	return r, nil
}

func (r *Runner) pump() {
	defer close(r.done)
	stats, err := r.cfg.Transport.Pump(r.cfg.Endpoint, r.cfg.Geometry,
		r.cfg.Packets, r.cfg.Depth, r.tone.Source(r.cfg.Duration), r.cancel)
	r.mu.Lock()
	r.stats = stats
	r.err = err
	r.live = false
	r.ended = time.Now()
	r.mu.Unlock()
}

// Stop cancels the run and waits for the pump to report. It returns the pump's
// error, which is nil for a cancelled or completed run.
func (r *Runner) Stop() error {
	select {
	case <-r.cancel:
	default:
		close(r.cancel)
	}
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Done reports whether the run ended by itself, which is what a bounded
// duration does.
func (r *Runner) Done() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Report compares the audio the pump finished with the clock the run took. On a
// device that is on clock the two agree within a few tens of parts per million;
// a schedule that rounded samples per frame would show up here as a drift the
// listener hears as pitch, not as an error code.
func (r *Runner) Report() Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Report{
		Running: r.live,
		Error:   "",
	}
	if r.err != nil {
		out.Error = r.err.Error()
	}
	elapsed := r.ended.Sub(r.started)
	if r.live {
		elapsed = time.Since(r.started)
	}
	out.ElapsedMS = elapsed.Milliseconds()
	if r.stats != nil {
		out.Bytes = r.stats.Bytes
		out.Submitted = r.stats.Submitted
		out.Completed = r.stats.Completed
		out.Failures = r.stats.Failures
		out.Short = r.stats.ShortSlots
		out.Packets = r.stats.Frames
		audio := r.stats.Duration(r.cfg.Geometry)
		out.AudioMS = audio.Milliseconds()
		if elapsed > 0 {
			out.DriftPPM = (audio.Seconds()/elapsed.Seconds() - 1) * 1e6
		}
	}
	return out
}

// Samples is the tone position: the whole sample frames the source produced.
// Compared with Report().AudioMS it says whether the pump asked for the sizes
// the schedule promised, since a packet that dropped a sample shows up as
// audio time the source never filled.
func (r *Runner) Samples() int { return r.tone.Samples() }
