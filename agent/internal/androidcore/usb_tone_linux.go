//go:build linux

package androidcore

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"hifihub/agent/internal/usbfs"
	"hifihub/agent/internal/usbplay"
)

// The exclusive-output prototype the plan asked for: claim the interface in
// Kotlin, hand the descriptor over, and run a known tone at the negotiated
// format. Everything here answers with JSON because that is what crosses JNI.
//
// usbfs.Handle is the transport: it drives isochronous URBs on the descriptor
// UsbDeviceConnection.getFileDescriptor() returned, past the Android mixer.

// toneRequest is one prototype run.
type toneRequest struct {
	FD         int       `json:"fd"`
	Endpoint   int       `json:"endpoint"`
	Rate       int       `json:"rate"`
	Channels   int       `json:"channels"`
	Subslot    int       `json:"subslot"`
	DurationMS int       `json:"duration_ms"`
	Tones      []float64 `json:"tones"`
	Amplitude  float64   `json:"amplitude"`
	Packets    int       `json:"packets"`
	Depth      int       `json:"depth"`
}

// toneAnswer is what the screen shows. It stays readable after the run ends, so
// a finished verdict does not disappear when the UI polls it again.
type toneAnswer struct {
	OK              bool            `json:"ok"`
	Error           string          `json:"error,omitempty"`
	Running         bool            `json:"running"`
	Speed           string          `json:"speed,omitempty"`
	FramesPerSecond int             `json:"frames_per_second,omitempty"`
	PeakPacketBytes int             `json:"peak_packet_bytes"`
	BytesPerSecond  int             `json:"bytes_per_second"`
	Samples         int             `json:"samples"`
	ElapsedMS       int64           `json:"elapsed_ms"`
	AudioMS         int64           `json:"audio_ms"`
	Delivered       int             `json:"delivered"`
	Failures        int             `json:"failures"`
	Short           int             `json:"short_packets"`
	DriftPPM        float64         `json:"drift_ppm"`
	Report          *usbplay.Report `json:"report,omitempty"`
}

// errNone and errRunning are the two states the screen has to tell apart from a
// device failure: nothing started yet, and a run still holding the endpoint.
var (
	errNone    = errors.New("androidcore: no USB tone run")
	errRunning = errors.New("androidcore: a USB tone run is already going")
)

// toneRun is the one run the terminal keeps: JNI has a single process, and a
// self test that could overlap two devices would report a false verdict.
type toneRun struct {
	mu     sync.Mutex
	runner *usbplay.Runner
	err    error
	speed  string
	geom   usbfs.Geometry
}

var tone toneRun

// answer renders the run as it stands. The caller holds the lock.
func (t *toneRun) answer() toneAnswer {
	out := toneAnswer{Speed: t.speed, FramesPerSecond: t.geom.FramesPerSecond}
	// Nothing has started, so the geometry is still the zero value and the
	// derived sizes would divide by a frame grid of none.
	if t.runner == nil {
		out.Error = errNone.Error()
		return out
	}
	out.OK = t.err == nil
	out.PeakPacketBytes = t.geom.PeakPacketBytes()
	out.BytesPerSecond = t.geom.BytesPerSecond()
	if t.err != nil {
		out.Error = t.err.Error()
	}
	rep := t.runner.Report()
	out.Running = rep.Running
	out.Samples = t.runner.Samples()
	out.ElapsedMS = rep.ElapsedMS
	out.AudioMS = rep.AudioMS
	out.Delivered = rep.Bytes
	out.Failures = rep.Failures
	out.Short = rep.Short
	out.DriftPPM = rep.DriftPPM
	out.Report = &rep
	return out
}

// UsbToneStart opens the descriptor and launches a tone run.
func UsbToneStart(in string) string {
	var req toneRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		return usbError(err)
	}
	tone.mu.Lock()
	defer tone.mu.Unlock()
	if tone.runner != nil && !tone.runner.Done() {
		return usbError(errRunning)
	}
	h, err := usbfs.New(req.FD)
	if err != nil {
		return usbJSON(toneAnswer{Error: err.Error()})
	}
	// The bus clock comes from the kernel rather than from the descriptor the
	// shell parsed: GET_SPEED is the authority on the frame grid the schedule
	// needs, and a high-speed device behind a full-speed hub is exactly the
	// case a descriptor cannot answer.
	fps, err := h.FramesPerSecond()
	if err != nil {
		return usbJSON(toneAnswer{Error: err.Error()})
	}
	speed, _ := h.BusSpeed()
	g := usbfs.Geometry{
		Rate:            req.Rate,
		Channels:        req.Channels,
		Subslot:         req.Subslot,
		FramesPerSecond: fps,
	}
	r, err := usbplay.Start(usbplay.RunConfig{
		Transport: h,
		Endpoint:  byte(req.Endpoint),
		Geometry:  g,
		Packets:   req.Packets,
		Depth:     req.Depth,
		Duration:  time.Duration(req.DurationMS) * time.Millisecond,
		Tones:     req.Tones,
		Amplitude: req.Amplitude,
	})
	if err != nil {
		return usbJSON(toneAnswer{Error: err.Error()})
	}
	tone.runner, tone.err, tone.speed, tone.geom = r, nil, speed, g
	return usbJSON(tone.answer())
}

// UsbToneStat reports the run in progress, or the one that finished.
func UsbToneStat() string {
	tone.mu.Lock()
	defer tone.mu.Unlock()
	return usbJSON(tone.answer())
}

// UsbToneStop ends the run and returns its final report. Releasing the USB
// interface is Kotlin's job, and doing that rebinds the kernel driver, which is
// what puts audio back on the AAudio path.
func UsbToneStop() string {
	tone.mu.Lock()
	defer tone.mu.Unlock()
	if tone.runner == nil {
		return usbJSON(tone.answer())
	}
	tone.err = tone.runner.Stop()
	return usbJSON(tone.answer())
}
