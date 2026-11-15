//go:build cgo && linux

package player

/*
#cgo pkg-config: alsa
#include <alsa/asoundlib.h>
#include <stdlib.h>
#include <errno.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"

	"hifihub/zat"
)

// alsaPlayer renders PCM through libasound. It opens the device in blocking
// mode with soft resampling disabled to keep the transport bit-perfect
// (PROTOCOL.md §9); the device must support the negotiated rate.
type alsaPlayer struct {
	handle     *C.snd_pcm_t
	frameBytes int
}

func newPlatform() Player { return &alsaPlayer{} }

func alsaErr(rc C.int) string { return C.GoString(C.snd_strerror(rc)) }

func (p *alsaPlayer) Open(format zat.Format, rate, channels int, device string) error {
	if channels <= 0 || rate <= 0 {
		return fmt.Errorf("player: invalid rate/channels %d/%d", rate, channels)
	}
	var sf C.snd_pcm_format_t
	switch format {
	case zat.S16LE:
		sf = C.SND_PCM_FORMAT_S16_LE
	case zat.S24LE:
		sf = C.SND_PCM_FORMAT_S24_3LE // 3-byte packed, matches zat S24_LE
	case zat.S32LE:
		sf = C.SND_PCM_FORMAT_S32_LE
	default:
		return fmt.Errorf("player: unsupported format %s", format)
	}
	if p.handle != nil {
		p.Close()
	}
	cdev := C.CString(device)
	defer C.free(unsafe.Pointer(cdev))
	var h *C.snd_pcm_t
	if rc := C.snd_pcm_open(&h, cdev, C.SND_PCM_STREAM_PLAYBACK, 0); rc < 0 {
		return fmt.Errorf("player: snd_pcm_open(%s): %s", device, alsaErr(rc))
	}
	// 500 ms target latency, no soft resample (bit-perfect).
	if rc := C.snd_pcm_set_params(h, sf, C.SND_PCM_ACCESS_RW_INTERLEAVED,
		C.uint(channels), C.uint(rate), 0, 500000); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: snd_pcm_set_params(%s): %s", device, alsaErr(rc))
	}
	p.handle = h
	p.frameBytes = format.BytesPerSample() * channels
	return nil
}

func (p *alsaPlayer) Write(pcm []byte) error {
	if p.handle == nil {
		return errors.New("player: device not open")
	}
	if p.frameBytes <= 0 {
		return errors.New("player: invalid frame size")
	}
	frames := len(pcm) / p.frameBytes
	offset := 0
	for offset < frames {
		n := C.snd_pcm_writei(p.handle,
			unsafe.Pointer(&pcm[offset*p.frameBytes]),
			C.snd_pcm_uframes_t(frames-offset))
		if n == -C.EPIPE { // underrun: recover and retry
			if rc := C.snd_pcm_prepare(p.handle); rc < 0 {
				return fmt.Errorf("player: prepare after underrun: %s", alsaErr(rc))
			}
			continue
		}
		if n < 0 {
			return fmt.Errorf("player: snd_pcm_writei: %s", alsaErr(C.int(n)))
		}
		offset += int(n)
	}
	return nil
}

func (p *alsaPlayer) Drain() error {
	if p.handle == nil {
		return nil
	}
	if rc := C.snd_pcm_drain(p.handle); rc < 0 {
		return fmt.Errorf("player: snd_pcm_drain: %s", alsaErr(rc))
	}
	return nil
}

func (p *alsaPlayer) Close() error {
	if p.handle == nil {
		return nil
	}
	C.snd_pcm_close(p.handle)
	p.handle = nil
	return nil
}
