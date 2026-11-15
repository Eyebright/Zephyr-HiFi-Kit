// Package decode turns audio files into interleaved PCM for ZAT streaming. The
// WAV path is pure Go and works everywhere; FLAC uses the CGO libFLAC backend on
// Linux (flac_cgo.go) and a stub elsewhere (flac_stub.go) so non-CGO builds and
// tests stay green. Other formats are identified by internal/media but not yet
// decodable in v0.1. See PROTOCOL.md §9.
package decode

import (
	"errors"
	"fmt"
	"io"

	"hifihub/server/internal/media"
	"hifihub/zat"
)

// PCMFormat describes the interleaved PCM a Decoder produces.
type PCMFormat struct {
	Format   zat.Format
	Rate     int
	Channels int
}

// BytesPerFrame is the byte width of one interleaved sample frame.
func (f PCMFormat) BytesPerFrame() int { return f.Format.BytesPerSample() * f.Channels }

// Decoder streams decoded PCM from an audio file. Read returns io.EOF once the
// track is exhausted; Format and TotalSamples are stable for the decoder's life.
type Decoder interface {
	io.ReadCloser
	Format() PCMFormat
	TotalSamples() uint64
}

// ErrUnsupported is returned for formats without a v0.1 decoder.
var ErrUnsupported = errors.New("decode: format not decodable in v0.1")

// Open probes path and returns a Decoder for it.
func Open(path string) (Decoder, error) {
	pr, err := media.ProbeFile(path)
	if err != nil {
		return nil, err
	}
	switch pr.Codec {
	case "flac":
		return openFLAC(path, pr)
	case "pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm":
		return openWAV(path)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, pr.Codec)
	}
}
