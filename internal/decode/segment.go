package decode

import (
	"fmt"
	"io"
)

// segmentDecoder exposes [start, end) samples of a source decoder as a new
// track with its own sample clock starting at zero. Leading samples are skipped
// once; the stream stops at the end of the window.
type segmentDecoder struct {
	Decoder
	start    uint64
	end      uint64
	produced uint64
	skipped  bool
}

// TotalSamples reports the window length, not the whole file.
func (d *segmentDecoder) TotalSamples() uint64 { return d.end - d.start }

// OpenSegment opens path and limits decoding to [start, end) samples.
func OpenSegment(path string, start, end uint64) (Decoder, error) {
	d, err := Open(path)
	if err != nil {
		return nil, err
	}
	total := d.TotalSamples()
	if end == 0 || end > total {
		end = total
	}
	if total == 0 || start >= end {
		d.Close()
		return nil, fmt.Errorf("decode: empty sample window %d-%d", start, end)
	}
	return &segmentDecoder{Decoder: d, start: start, end: end}, nil
}

func (d *segmentDecoder) Read(p []byte) (int, error) {
	bpf := uint64(d.Format().BytesPerFrame())
	if bpf == 0 {
		return 0, fmt.Errorf("decode: invalid frame size")
	}
	if !d.skipped {
		var err error
		if sk, ok := d.Decoder.(interface{ SeekSample(uint64) error }); ok {
			err = sk.SeekSample(d.start)
		} else {
			_, err = io.CopyN(io.Discard, d.Decoder, int64(d.start*bpf))
		}
		if err != nil {
			return 0, err
		}
		d.skipped = true
	}
	window := d.end - d.start
	if d.produced >= window {
		return 0, io.EOF
	}
	if remain := (window - d.produced) * bpf; uint64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := d.Decoder.Read(p)
	d.produced += uint64(n) / bpf
	return n, err
}
