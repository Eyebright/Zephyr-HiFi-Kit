//go:build !(cgo && linux)

package decode

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"

	"hifihub/server/internal/media"
	"hifihub/zat"
)

// flacDecoder is the pure-Go FLAC path used on Windows and in non-CGO builds.
// It produces interleaved PCM for ZAT just like the CGO libFLAC backend.
type flacDecoder struct {
	stream *flac.Stream
	file   *os.File
	format PCMFormat
	total  uint64
	buf    []byte
	off    int
}

func openFLAC(path string, pr media.ProbeResult) (Decoder, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	stream, err := flac.NewSeek(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("decode: open FLAC: %w", err)
	}
	info := stream.Info
	bits := int(info.BitsPerSample)
	var format zat.Format
	switch {
	case bits > 0 && bits <= 16:
		format = zat.S16LE
	case bits <= 24:
		format = zat.S24LE
	case bits <= 32:
		format = zat.S32LE
	default:
		stream.Close()
		f.Close()
		return nil, fmt.Errorf("decode: unsupported FLAC bit depth %d", bits)
	}
	rate, channels := int(info.SampleRate), int(info.NChannels)
	if rate == 0 {
		rate = pr.Rate
	}
	if channels == 0 {
		channels = pr.Channels
	}
	total := info.NSamples
	if total == 0 {
		total = pr.TotalSamples
	}
	return &flacDecoder{
		stream: stream,
		file:   f,
		format: PCMFormat{Format: format, Rate: rate, Channels: channels},
		total:  total,
	}, nil
}

func (d *flacDecoder) Format() PCMFormat    { return d.format }
func (d *flacDecoder) TotalSamples() uint64 { return d.total }

func (d *flacDecoder) Read(p []byte) (int, error) {
	for d.off >= len(d.buf) {
		frame, err := d.stream.ParseNext()
		if err != nil {
			if err == io.EOF {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("decode: FLAC frame: %w", err)
		}
		d.buf = d.interleave(frame)
		d.off = 0
		if len(d.buf) == 0 {
			continue
		}
	}
	n := copy(p, d.buf[d.off:])
	d.off += n
	return n, nil
}

func (d *flacDecoder) interleave(f *frame.Frame) []byte {
	nch, ns := len(f.Subframes), int(f.BlockSize)
	if nch == 0 || ns == 0 {
		return nil
	}
	bps := d.format.Format.BytesPerSample()
	targetBits := bps * 8
	shift := 0
	if src := int(d.stream.Info.BitsPerSample); src > 0 && targetBits > src {
		shift = targetBits - src
	}
	out := make([]byte, ns*nch*bps)
	idx := 0
	for i := 0; i < ns; i++ {
		for ch := 0; ch < nch; ch++ {
			s := f.Subframes[ch].Samples[i] << shift
			switch d.format.Format {
			case zat.S16LE:
				binary.LittleEndian.PutUint16(out[idx:], uint16(int16(s)))
			case zat.S24LE:
				u := uint32(s)
				out[idx], out[idx+1], out[idx+2] = byte(u), byte(u>>8), byte(u>>16)
			case zat.S32LE:
				binary.LittleEndian.PutUint32(out[idx:], uint32(s))
			}
			idx += bps
		}
	}
	return out
}

func (d *flacDecoder) Close() error {
	if d.stream != nil {
		_ = d.stream.Close()
	}
	if d.file != nil {
		return d.file.Close()
	}
	return nil
}
