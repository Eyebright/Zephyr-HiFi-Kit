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
	"unsafe"

	"hifihub/zat"
)

// aaudioPlayer renders PCM through Android's AAudio (NDK, API 26+). It mirrors
// the ALSA backend: blocking writes pace the source at the sample clock, and
// EXCLUSIVE/LOW_LATENCY sharing keeps the transport bit-perfect (PROTOCOL.md §9).
// AAudio has no ALSA device names; the negotiated device string is ignored and
// the default output is used. S24_LE maps to AAUDIO_FORMAT_PCM_I24_PACKED (3-byte
// packed little-endian), matching the wire format exactly.
type aaudioPlayer struct {
	stream     *C.AAudioStream
	frameBytes int
}

func newPlatform() Player { return &aaudioPlayer{} }

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

// openStream builds and opens an AAudio output stream. It first requests
// EXCLUSIVE sharing (bit-perfect, MMAP where available); if the device refuses
// exclusive mode it falls back to SHARED so playback still works.
func openAAudio(sf C.aaudio_format_t, rate, channels int, exclusive bool) (*C.AAudioStream, error) {
	var b *C.AAudioStreamBuilder
	if rc := C.AAudio_createStreamBuilder(&b); rc != C.AAUDIO_OK {
		return nil, fmt.Errorf("player: AAudio_createStreamBuilder: %d", int(rc))
	}
	defer C.AAudioStreamBuilder_delete(b)
	C.AAudioStreamBuilder_setDirection(b, C.AAUDIO_DIRECTION_OUTPUT)
	C.AAudioStreamBuilder_setFormat(b, sf)
	C.AAudioStreamBuilder_setSampleRate(b, C.int32_t(rate))
	C.AAudioStreamBuilder_setChannelCount(b, C.int32_t(channels))
	C.AAudioStreamBuilder_setPerformanceMode(b, C.AAUDIO_PERFORMANCE_MODE_LOW_LATENCY)
	// setUsage/setContentType are API 28+; omitted to keep the minSdk 26 (Android
	// 8.0) floor. The default output attributes are correct for music playback.
	if exclusive {
		C.AAudioStreamBuilder_setSharingMode(b, C.AAUDIO_SHARING_MODE_EXCLUSIVE)
	} else {
		C.AAudioStreamBuilder_setSharingMode(b, C.AAUDIO_SHARING_MODE_SHARED)
	}
	var s *C.AAudioStream
	if rc := C.AAudioStreamBuilder_openStream(b, &s); rc != C.AAUDIO_OK {
		return nil, fmt.Errorf("player: AAudioStreamBuilder_openStream: %d", int(rc))
	}
	return s, nil
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
	s, err := openAAudio(sf, rate, channels, true)
	if err != nil {
		// Exclusive mode is unavailable on some devices; fall back to shared.
		s, err = openAAudio(sf, rate, channels, false)
		if err != nil {
			return err
		}
	}
	if rc := C.AAudioStream_requestStart(s); rc != C.AAUDIO_OK {
		C.AAudioStream_close(s)
		return fmt.Errorf("player: AAudioStream_requestStart: %d", int(rc))
	}
	p.stream = s
	p.frameBytes = format.BytesPerSample() * channels
	return nil
}

func (p *aaudioPlayer) Write(pcm []byte) error {
	if p.stream == nil {
		return errors.New("player: device not open")
	}
	if p.frameBytes <= 0 {
		return errors.New("player: invalid frame size")
	}
	frames := len(pcm) / p.frameBytes
	offset := 0
	for offset < frames {
		n := C.AAudioStream_write(p.stream,
			unsafe.Pointer(&pcm[offset*p.frameBytes]),
			C.int32_t(frames-offset), C.int64_t(200000000)) // 200 ms blocking timeout
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

// Drain stops the stream so pending samples are flushed to the output. AAudio
// has no ALSA-style drain; requestStop completes the buffered frames.
func (p *aaudioPlayer) Drain() error {
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
