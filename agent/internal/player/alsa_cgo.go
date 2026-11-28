//go:build cgo && linux && !android

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
	"strings"
	"unsafe"

	"hifihub/zat"
)

// alsaPlayer renders PCM through libasound. The strict hw: device is tried
// first with soft resampling disabled; if it rejects the negotiated format, the
// same card is reopened through ALSA's plug plugin so the device can convert.
type alsaPlayer struct {
	handle     *C.snd_pcm_t
	frameBytes int
	// pack24to32 is set when the hw device only accepts 32-bit slots; the 24
	// payload bits are shifted into the high bytes with zero padding.
	pack24to32 bool
	pack16to32 bool
	bitPerfect bool
	detail     string
}

func newPlatform() Player { return &alsaPlayer{} }

func supportsBitPerfect() bool { return true }

// OpenBitPerfect opens only an hw: PCM with no soft resampling and no plug
// fallback. A 24-bit stream may be left-aligned into 32-bit slots because that
// preserves the exact 24 payload bits; nothing else is converted.
func (p *alsaPlayer) OpenBitPerfect(format zat.Format, rate, channels int, device string) error {
	if channels <= 0 || rate <= 0 {
		return fmt.Errorf("player: invalid rate/channels %d/%d", rate, channels)
	}
	if !strings.HasPrefix(device, "hw:") {
		return fmt.Errorf("player: MQA 位完美输出要求 hw: 直连设备，当前 %s", device)
	}
	var sf C.snd_pcm_format_t
	switch format {
	case zat.S16LE:
		sf = C.SND_PCM_FORMAT_S16_LE
	case zat.S24LE:
		sf = C.SND_PCM_FORMAT_S24_3LE
	case zat.S32LE:
		sf = C.SND_PCM_FORMAT_S32_LE
	default:
		return fmt.Errorf("player: unsupported format %s", format)
	}
	if p.handle != nil {
		p.Close()
	}
	p.pack24to32, p.pack16to32, p.bitPerfect, p.detail = false, false, false, ""
	p.frameBytes = format.BytesPerSample() * channels
	if err := p.openDevice(sf, rate, channels, device); err == nil {
		p.bitPerfect = true
		return nil
	}
	// Many USB DACs expose only 32-bit slots. Widen the payload into the high
	// bits with zero padding; every source bit is preserved.
	if format == zat.S24LE || format == zat.S16LE {
		if err := p.openDevice(C.SND_PCM_FORMAT_S32_LE, rate, channels, device); err == nil {
			p.frameBytes = 4 * channels
			p.bitPerfect = true
			if format == zat.S24LE {
				p.pack24to32 = true
				p.detail = "24-bit 左对齐装入 32-bit hw"
			} else {
				p.pack16to32 = true
				p.detail = "16-bit 左对齐装入 32-bit hw"
			}
			return nil
		}
	}
	p.frameBytes = 0
	return fmt.Errorf("player: MQA 需要位完美 hw 输出，%s 不接受 %s/%dHz（请选择直连 DAC 的 hw 设备）", device, format, rate)
}

// BitPerfect reports whether the current open is a strict, non-converting path.
func (p *alsaPlayer) BitPerfect() (bool, string) { return p.bitPerfect, p.detail }

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
	p.pack24to32, p.pack16to32, p.bitPerfect, p.detail = false, false, false, ""
	p.frameBytes = format.BytesPerSample() * channels
	if err := p.openDevice(sf, rate, channels, device); err != nil {
		if fallback, ok := plugDevice(device); ok {
			if ferr := p.openDevice(sf, rate, channels, fallback); ferr != nil {
				return fmt.Errorf("%w; fallback %s failed: %v", err, fallback, ferr)
			}
			return nil
		}
		return err
	}
	return nil
}

func (p *alsaPlayer) openDevice(sf C.snd_pcm_format_t, rate, channels int, device string) error {
	cdev := C.CString(device)
	defer C.free(unsafe.Pointer(cdev))
	var h *C.snd_pcm_t
	if rc := C.snd_pcm_open(&h, cdev, C.SND_PCM_STREAM_PLAYBACK, 0); rc < 0 {
		return fmt.Errorf("player: snd_pcm_open(%s): %s", device, alsaErr(rc))
	}
	// 500 ms target latency; the strict device path does no soft resampling.
	if rc := C.snd_pcm_set_params(h, sf, C.SND_PCM_ACCESS_RW_INTERLEAVED,
		C.uint(channels), C.uint(rate), 0, 500000); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: snd_pcm_set_params(%s): %s", device, alsaErr(rc))
	}
	// snd_pcm_set_params leaves start_threshold at the full buffer, so after an
	// underrun prepare (or a pause that drained the queue) sub-buffer writes
	// never restart playback. Start on one period instead.
	var bufSize, periodSize C.snd_pcm_uframes_t
	if rc := C.snd_pcm_get_params(h, &bufSize, &periodSize); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: snd_pcm_get_params(%s): %s", device, alsaErr(rc))
	}
	var sw *C.snd_pcm_sw_params_t
	if rc := C.snd_pcm_sw_params_malloc(&sw); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: sw_params_malloc: %s", alsaErr(rc))
	}
	defer C.snd_pcm_sw_params_free(sw)
	if rc := C.snd_pcm_sw_params_current(h, sw); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: sw_params_current(%s): %s", device, alsaErr(rc))
	}
	if rc := C.snd_pcm_sw_params_set_start_threshold(h, sw, periodSize); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: set_start_threshold(%s): %s", device, alsaErr(rc))
	}
	if rc := C.snd_pcm_sw_params(h, sw); rc < 0 {
		C.snd_pcm_close(h)
		return fmt.Errorf("player: sw_params(%s): %s", device, alsaErr(rc))
	}
	p.handle = h
	return nil
}

func (p *alsaPlayer) Write(pcm []byte) error {
	if p.handle == nil {
		return errors.New("player: device not open")
	}
	if p.frameBytes <= 0 {
		return errors.New("player: invalid frame size")
	}
	if p.pack24to32 {
		pcm = pack24to32(pcm)
	} else if p.pack16to32 {
		pcm = pack16to32(pcm)
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

func (p *alsaPlayer) Drop() error {
	if p.handle == nil {
		return nil
	}
	if rc := C.snd_pcm_drop(p.handle); rc < 0 {
		return fmt.Errorf("player: snd_pcm_drop: %s", alsaErr(rc))
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
