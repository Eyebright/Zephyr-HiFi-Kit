// Package mqa identifies MQA-encoded PCM. MQA (including MQA-CD) hides a
// 36-bit sync word in the least significant bits of the stereo difference
// signal, so detection needs decoded samples rather than container tags.
// Detection is only identification: MQA unfolding stays in the DAC.
package mqa

import (
	"encoding/binary"
	"io"

	"hifihub/server/internal/decode"
	"hifihub/zat"
)

// magic is the 36-bit MQA sync word.
const magic uint64 = 0xbe0498c88

// scanSeconds bounds detection cost; the sync word appears at the start.
const scanSeconds = 3

// Result is the outcome of an MQA scan.
type Result struct {
	Detected     bool
	Studio       bool
	OriginalRate int // restored sample rate in Hz, 0 when unknown
	BitPosition  int // bit plane carrying the sync word (16..23)
	SyncPosition int // sample index where the sync word completed
	Provenance   int
}

// AnalyzeSamples scans interleaved stereo samples that are left-justified in
// int32 (16-bit samples shifted by 16, 24-bit by 8), matching how 24-bit MQA and
// 16-bit MQA-CD share the same bit planes.
func AnalyzeSamples(interleaved []int32, maxFrames int) Result {
	if len(interleaved) < 4 {
		return Result{}
	}
	frames := len(interleaved) / 2
	if maxFrames > 0 && frames > maxFrames {
		frames = maxFrames
	}
	for pos := 16; pos <= 23; pos++ {
		var buf uint64
		for i := 0; i < frames; i++ {
			bit := uint64((interleaved[2*i]^interleaved[2*i+1])>>pos) & 1
			buf = (buf<<1 | bit) & 0xFFFFFFFFF
			if buf != magic {
				continue
			}
			prov := extract(interleaved, i+29, 5, pos)
			return Result{
				Detected:     true,
				Studio:       prov > 8,
				OriginalRate: decodeRate(extract(interleaved, i+3, 4, pos)),
				BitPosition:  pos,
				SyncPosition: i,
				Provenance:   prov,
			}
		}
	}
	return Result{}
}

func extract(interleaved []int32, start, n, pos int) int {
	out := 0
	for i := 0; i < n; i++ {
		idx := start + i
		if idx*2+1 >= len(interleaved) {
			break
		}
		bit := (interleaved[idx*2] ^ interleaved[idx*2+1]) >> pos & 1
		out = out<<1 | int(bit)
	}
	return out
}

// decodeRate turns the 4-bit original sample-rate code into Hz.
func decodeRate(code int) int {
	base := 44100
	if code&1 == 1 {
		base = 48000
	}
	mult := 1 << ((code >> 1) & 7)
	if mult > 16 {
		mult *= 2
	}
	return base * mult
}

// Decode converts a PCM buffer to left-justified int32 samples.
func Decode(pcm []byte, format zat.Format) []int32 {
	bps := format.BytesPerSample()
	if bps == 0 {
		return nil
	}
	out := make([]int32, 0, len(pcm)/bps)
	for i := 0; i+bps <= len(pcm); i += bps {
		switch bps {
		case 2:
			out = append(out, int32(int16(binary.LittleEndian.Uint16(pcm[i:])))<<16)
		case 3:
			v := int32(pcm[i]) | int32(pcm[i+1])<<8 | int32(pcm[i+2])<<16
			if v&0x800000 != 0 {
				v |= ^0xFFFFFF
			}
			out = append(out, v<<8)
		case 4:
			out = append(out, int32(binary.LittleEndian.Uint32(pcm[i:])))
		default:
			return nil
		}
	}
	return out
}

// DetectFile opens path with the normal decoder, scans the first seconds and
// returns the MQA result. Non-stereo or unsupported files yield a zero Result.
func DetectFile(path string) (Result, error) {
	d, err := decode.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer d.Close()
	f := d.Format()
	if f.Channels != 2 || f.Rate <= 0 {
		return Result{}, nil
	}
	want := f.Rate * scanSeconds * 2
	samples := make([]int32, 0, want)
	buf := make([]byte, 64*1024)
	for len(samples) < want {
		n, err := d.Read(buf)
		if n > 0 {
			samples = append(samples, Decode(buf[:n], f.Format)...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, err
		}
	}
	return AnalyzeSamples(samples, f.Rate*scanSeconds), nil
}
