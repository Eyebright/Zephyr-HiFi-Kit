//go:build cgo && android

package player

/*
#cgo LDFLAGS: -laaudio
#include <aaudio/AAudio.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unsafe"

	"hifihub/zat"
)

// aaudioPlayer renders PCM through Android's AAudio (NDK, API 26+). It mirrors
// the ALSA backend: blocking writes pace the source at the sample clock. SHARED
// sharing lets the Android mixer resample to the device's native clock so every
// source rate plays reliably (PROTOCOL.md §9); AAudio cannot guarantee MQA bits.
// AAudio has no ALSA device names; the negotiated device string is ignored and
// the default output is used. S24_LE maps to AAUDIO_FORMAT_PCM_I24_PACKED (3-byte
// packed little-endian), matching the wire format exactly.
type aaudioPlayer struct {
	stream     *C.AAudioStream
	frameBytes int
	sf         C.aaudio_format_t
	srcFormat  zat.Format
	srcRate    int
	channels   int
	rs         *resampler
	opened     openCombo
	floatOut   bool
}

// openCombo records the builder parameters that actually opened on this
// device so reconnects reuse them instead of negotiating from scratch.
type openCombo struct {
	rate int // 0 leaves the rate unset so AAudio picks the device native clock
	perf C.aaudio_performance_mode_t
	sf   C.aaudio_format_t
}

func newPlatform() Player { return &aaudioPlayer{} }

// AAudio may route through the Android mixer, so it cannot guarantee MQA bits.
func supportsBitPerfect() bool { return false }

func aaudioFormat(f zat.Format) (C.aaudio_format_t, error) {
	switch f {
	case zat.S16LE:
		return C.AAUDIO_FORMAT_PCM_I16, nil
	case zat.S24LE:
		return C.AAUDIO_FORMAT_PCM_I24_PACKED, nil
	case zat.S32LE:
		return C.AAUDIO_FORMAT_PCM_I32, nil
	default:
		return 0, fmt.Errorf("player: unsupported format %s", f)
	}
}

// openAAudio builds and opens an AAudio output stream in SHARED mode. A rate of
// 0 leaves the sample rate unset so AAudio picks the device's native clock; the
// caller then resamples the source to that rate. Forcing a specific rate makes
// USB DACs re-enumerate and drop the stream (AAUDIO_ERROR_DISCONNECTED).
func openAAudio(sf C.aaudio_format_t, rate, channels int, exclusive bool, perf C.aaudio_performance_mode_t) (*C.AAudioStream, C.aaudio_result_t) {
	var b *C.AAudioStreamBuilder
	if rc := C.AAudio_createStreamBuilder(&b); rc != C.AAUDIO_OK {
		return nil, rc
	}
	defer C.AAudioStreamBuilder_delete(b)
	C.AAudioStreamBuilder_setDirection(b, C.AAUDIO_DIRECTION_OUTPUT)
	C.AAudioStreamBuilder_setFormat(b, sf)
	if rate > 0 {
		C.AAudioStreamBuilder_setSampleRate(b, C.int32_t(rate))
	}
	C.AAudioStreamBuilder_setChannelCount(b, C.int32_t(channels))
	C.AAudioStreamBuilder_setPerformanceMode(b, perf)
	// setUsage/setContentType are API 28+; omitted to keep the minSdk 26 (Android
	// 8.0) floor. The default output attributes are correct for music playback.
	if exclusive {
		C.AAudioStreamBuilder_setSharingMode(b, C.AAUDIO_SHARING_MODE_EXCLUSIVE)
	} else {
		C.AAudioStreamBuilder_setSharingMode(b, C.AAUDIO_SHARING_MODE_SHARED)
	}
	var s *C.AAudioStream
	if rc := C.AAudioStreamBuilder_openStream(b, &s); rc != C.AAUDIO_OK {
		return nil, rc
	}
	return s, C.AAUDIO_OK
}

// openStreamNegotiated walks a ladder of builder combinations because devices
// disagree on what openStream accepts: some reject PERFORMANCE_MODE_NONE or an
// unset sample rate with AAUDIO_ERROR_ILLEGAL_ARGUMENT (-898), and pre-API-31
// platforms reject the packed 24/32-bit integer formats. The first combination
// that opens wins and is remembered for reconnects.
func (p *aaudioPlayer) openStreamNegotiated(rate, channels int) (*C.AAudioStream, error) {
	ladder := []openCombo{
		{0, C.AAUDIO_PERFORMANCE_MODE_NONE, p.sf},
		{rate, C.AAUDIO_PERFORMANCE_MODE_LOW_LATENCY, p.sf},
		{0, C.AAUDIO_PERFORMANCE_MODE_LOW_LATENCY, p.sf},
	}
	if p.sf != C.AAUDIO_FORMAT_PCM_FLOAT {
		ladder = append(ladder,
			openCombo{0, C.AAUDIO_PERFORMANCE_MODE_NONE, C.AAUDIO_FORMAT_PCM_FLOAT},
			openCombo{rate, C.AAUDIO_PERFORMANCE_MODE_LOW_LATENCY, C.AAUDIO_FORMAT_PCM_FLOAT})
	}
	if p.opened.sf != 0 {
		ladder = append([]openCombo{p.opened}, ladder...)
	}
	var lastRC C.aaudio_result_t
	for _, c := range ladder {
		s, rc := openAAudio(c.sf, c.rate, channels, false, c.perf)
		if rc == C.AAUDIO_OK {
			p.opened = c
			slog.Info("aaudio stream opened", "requested_rate", c.rate, "performance_mode", int(c.perf), "format", int(c.sf))
			return s, nil
		}
		lastRC = rc
		if rc != C.AAUDIO_ERROR_ILLEGAL_ARGUMENT && rc != C.AAUDIO_ERROR_INVALID_FORMAT {
			break
		}
	}
	return nil, fmt.Errorf("player: AAudioStreamBuilder_openStream: %d", int(lastRC))
}

func (p *aaudioPlayer) Open(format zat.Format, rate, channels int, device string) error {
	if channels <= 0 || rate <= 0 {
		return fmt.Errorf("player: invalid rate/channels %d/%d", rate, channels)
	}
	sf, err := aaudioFormat(format)
	if err != nil {
		return err
	}
	if p.stream != nil {
		p.Close()
	}
	// Open at the device's native clock (rate 0) instead of forcing the source
	// rate: forcing a new rate makes USB DACs re-enumerate and drop the stream.
	// The source is then resampled to whatever rate the stream actually runs at.
	p.sf = sf
	p.opened = openCombo{}
	s, err := p.openStreamNegotiated(rate, channels)
	if err != nil {
		return err
	}
	if rc := C.AAudioStream_requestStart(s); rc != C.AAUDIO_OK {
		C.AAudioStream_close(s)
		return fmt.Errorf("player: AAudioStream_requestStart: %d", int(rc))
	}
	p.stream = s
	p.configure(format, rate, channels)
	return nil
}

// configure records the source format and builds a resampler when the stream's
// actual rate differs from the source rate.
func (p *aaudioPlayer) configure(format zat.Format, rate, channels int) {
	p.srcFormat, p.srcRate, p.channels = format, rate, channels
	p.floatOut = C.AAudioStream_getFormat(p.stream) == C.AAUDIO_FORMAT_PCM_FLOAT
	bps := format.BytesPerSample()
	if p.floatOut {
		bps = 4
	}
	p.frameBytes = bps * channels
	p.rs = nil
	streamRate := int(C.AAudioStream_getSampleRate(p.stream))
	if streamRate > 0 && streamRate != rate {
		p.rs = newResampler(rate, streamRate, channels, format.BytesPerSample())
	}
}

// ActualSampleRate reports the rate the opened stream is really running at.
// In SHARED mode AAudio resamples to the device's native clock, which can differ
// from the requested source rate (e.g. a DAC fixed at 768 kHz).
func (p *aaudioPlayer) ActualSampleRate() int {
	if p.stream == nil {
		return 0
	}
	return int(C.AAudioStream_getSampleRate(p.stream))
}

func (p *aaudioPlayer) Write(pcm []byte) error {
	if p.stream == nil {
		return errors.New("player: device not open")
	}
	if p.frameBytes <= 0 {
		return errors.New("player: invalid frame size")
	}
	// Convert the source to the stream's native rate when they differ; the
	// resampler carries state across calls so the stream stays continuous.
	if p.rs != nil {
		pcm = p.rs.process(pcm)
		if len(pcm) == 0 {
			return nil
		}
	}
	if p.floatOut {
		pcm = pcmToFloat(pcm, p.srcFormat)
		if len(pcm) == 0 {
			return nil
		}
	}
	frames := len(pcm) / p.frameBytes
	offset := 0
	reopens := 0
	for offset < frames {
		n := C.AAudioStream_write(p.stream,
			unsafe.Pointer(&pcm[offset*p.frameBytes]),
			C.int32_t(frames-offset), C.int64_t(200000000)) // 200 ms blocking timeout
		if n == C.AAUDIO_ERROR_DISCONNECTED {
			// The output device was invalidated (a USB DAC re-enumerates when its
			// clock changes). AAudio's documented recovery is to close and reopen
			// the stream; retry a few times before giving up so a transient
			// disconnect does not abort playback.
			if reopens >= 3 {
				return fmt.Errorf("player: AAudioStream_write: %d (device disconnected)", int(n))
			}
			reopens++
			if err := p.reopen(); err != nil {
				return err
			}
			continue
		}
		if n < 0 {
			return fmt.Errorf("player: AAudioStream_write: %d", int(n))
		}
		if n == 0 {
			return errors.New("player: AAudioStream_write stalled")
		}
		offset += int(n)
	}
	return nil
}

// reopen closes and reopens the stream after a disconnect, giving the device a
// moment to settle (USB re-enumeration) before reopening at its native clock.
func (p *aaudioPlayer) reopen() error {
	if p.stream != nil {
		C.AAudioStream_requestStop(p.stream)
		C.AAudioStream_close(p.stream)
		p.stream = nil
	}
	time.Sleep(200 * time.Millisecond)
	s, err := p.openStreamNegotiated(p.srcRate, p.channels)
	if err != nil {
		return err
	}
	if rc := C.AAudioStream_requestStart(s); rc != C.AAUDIO_OK {
		C.AAudioStream_close(s)
		return fmt.Errorf("player: AAudioStream_requestStart: %d", int(rc))
	}
	p.stream = s
	p.configure(p.srcFormat, p.srcRate, p.channels)
	return nil
}

// Drain stops the stream so pending samples are flushed to the output. AAudio
// has no ALSA-style drain; requestStop completes the buffered frames.
func (p *aaudioPlayer) Drain() error {
	if p.stream == nil {
		return nil
	}
	C.AAudioStream_requestStop(p.stream)
	return nil
}

func (p *aaudioPlayer) Drop() error {
	if p.stream == nil {
		return nil
	}
	C.AAudioStream_requestStop(p.stream)
	return nil
}

func (p *aaudioPlayer) Close() error {
	if p.stream == nil {
		return nil
	}
	C.AAudioStream_requestStop(p.stream)
	C.AAudioStream_close(p.stream)
	p.stream = nil
	return nil
}
