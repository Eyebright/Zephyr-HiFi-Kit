//go:build cgo && linux

package decode

/*
#cgo pkg-config: flac
#include <FLAC/stream_decoder.h>
#include <stdlib.h>
#include <stdio.h>

extern FLAC__StreamDecoder* zatFLACNew(void);
extern FLAC__StreamDecoderInitStatus zatFLACInitFILE(FLAC__StreamDecoder* d, FILE* f, void* cd);
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"

	"hifihub/server/internal/media"
	"hifihub/zat"
)

// flacDecoder decodes FLAC via libFLAC's stream decoder. The write callback
// packs samples into buf; Read drains it, decoding more frames on demand. Both
// run on the same goroutine (libFLAC is synchronous), so no lock guards buf.
type flacDecoder struct {
	dec          *C.FLAC__StreamDecoder
	file         *C.FILE
	handle       uintptr
	format       PCMFormat
	totalSamples uint64
	buf          []byte
	finished     bool
	err          error
}

// client_data carries an integer handle (not a Go pointer) to satisfy cgo's
// pointer-passing rules; the registry maps it back to the decoder.
var (
	flacMu   sync.Mutex
	flacReg  = map[uintptr]*flacDecoder{}
	flacNext uintptr
)

func registerFLAC(d *flacDecoder) uintptr {
	flacMu.Lock()
	defer flacMu.Unlock()
	flacNext++
	flacReg[flacNext] = d
	return flacNext
}

func lookupFLAC(cd unsafe.Pointer) *flacDecoder {
	flacMu.Lock()
	defer flacMu.Unlock()
	return flacReg[uintptr(cd)]
}

func unregisterFLAC(h uintptr) {
	flacMu.Lock()
	defer flacMu.Unlock()
	delete(flacReg, h)
}

func openFLAC(path string, pr media.ProbeResult) (Decoder, error) {
	format := PCMFormat{Rate: pr.Rate, Channels: pr.Channels}
	switch pr.Bits {
	case 16:
		format.Format = zat.S16LE
	case 24:
		format.Format = zat.S24LE
	case 32:
		format.Format = zat.S32LE
	default:
		return nil, fmt.Errorf("decode: unsupported FLAC bit depth %d", pr.Bits)
	}
	if format.Channels <= 0 || format.Rate <= 0 {
		return nil, errors.New("decode: invalid FLAC stream info")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	cmode := C.CString("rb")
	defer C.free(unsafe.Pointer(cmode))
	f := C.fopen(cpath, cmode)
	if f == nil {
		return nil, fmt.Errorf("decode: cannot open %s", path)
	}
	dec := C.zatFLACNew()
	if dec == nil {
		C.fclose(f)
		return nil, errors.New("decode: FLAC__stream_decoder_new failed")
	}
	d := &flacDecoder{dec: dec, file: f, format: format, totalSamples: pr.TotalSamples}
	d.handle = registerFLAC(d)
	if st := C.zatFLACInitFILE(dec, f, unsafe.Pointer(d.handle)); st != C.FLAC__STREAM_DECODER_INIT_STATUS_OK {
		unregisterFLAC(d.handle)
		C.FLAC__stream_decoder_delete(dec)
		C.fclose(f)
		return nil, fmt.Errorf("decode: FLAC init failed (%d)", int(st))
	}
	return d, nil
}

func (d *flacDecoder) Format() PCMFormat    { return d.format }
func (d *flacDecoder) TotalSamples() uint64 { return d.totalSamples }

func (d *flacDecoder) Read(p []byte) (int, error) {
	for len(d.buf) == 0 && !d.finished && d.err == nil {
		if C.FLAC__stream_decoder_process_single(d.dec) == 0 {
			d.err = errors.New("decode: FLAC process_single failed")
			break
		}
		if C.FLAC__stream_decoder_get_state(d.dec) == C.FLAC__STREAM_DECODER_END_OF_STREAM {
			d.finished = true
		}
	}
	if len(d.buf) > 0 {
		n := copy(p, d.buf)
		d.buf = d.buf[n:]
		return n, nil
	}
	if d.err != nil {
		return 0, d.err
	}
	return 0, io.EOF
}

func (d *flacDecoder) Close() error {
	if d.dec != nil {
		C.FLAC__stream_decoder_finish(d.dec)
		C.FLAC__stream_decoder_delete(d.dec)
		d.dec = nil
	}
	if d.file != nil {
		C.fclose(d.file)
		d.file = nil
	}
	unregisterFLAC(d.handle)
	return nil
}

//export goFLACWrite
func goFLACWrite(_ *C.FLAC__StreamDecoder, f *C.FLAC__Frame, b **C.FLAC__int32, cd unsafe.Pointer) C.FLAC__StreamDecoderWriteStatus {
	d := lookupFLAC(cd)
	if d == nil {
		return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
	}
	channels := int(f.header.channels)
	blocksize := int(f.header.blocksize)
	bps := d.format.Format.BytesPerSample()
	if channels <= 0 || bps == 0 {
		return C.FLAC__STREAM_DECODER_WRITE_STATUS_ABORT
	}
	for i := 0; i < blocksize; i++ {
		for ch := 0; ch < channels; ch++ {
			chPtr := (*[1 << 28]*C.FLAC__int32)(unsafe.Pointer(b))[ch]
			v := (*[1 << 28]C.FLAC__int32)(unsafe.Pointer(chPtr))[i]
			switch bps {
			case 2:
				d.buf = append(d.buf, byte(v), byte(v>>8))
			case 3:
				d.buf = append(d.buf, byte(v), byte(v>>8), byte(v>>16))
			case 4:
				d.buf = append(d.buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
			}
		}
	}
	return C.FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE
}

//export goFLACMeta
func goFLACMeta(dec *C.FLAC__StreamDecoder, meta *C.FLAC__StreamMetadata, cd unsafe.Pointer) {}

//export goFLACErr
func goFLACErr(_ *C.FLAC__StreamDecoder, s C.FLAC__StreamDecoderErrorStatus, cd unsafe.Pointer) {
	if d := lookupFLAC(cd); d != nil {
		d.err = fmt.Errorf("decode: FLAC stream error %d", int(s))
	}
}
