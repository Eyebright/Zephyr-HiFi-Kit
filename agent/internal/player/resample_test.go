package player

import (
	"encoding/binary"
	"math"
	"testing"
)

func rampS16(n int) []byte {
	b := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(i)))
	}
	return b
}

func readS16(b []byte, i int) int { return int(int16(binary.LittleEndian.Uint16(b[i*2:]))) }

func TestResamplerRampUpsample(t *testing.T) {
	r := newResampler(48000, 96000, 1, 2) // step 0.5
	out := r.process(rampS16(100))
	frames := len(out) / 2
	if frames < 190 {
		t.Fatalf("upsample frames = %d, want ~199", frames)
	}
	for i := 0; i < frames; i++ {
		want := float64(i) * 0.5
		if math.Abs(float64(readS16(out, i))-want) > 1 {
			t.Fatalf("frame %d = %d, want ~%.1f", i, readS16(out, i), want)
		}
	}
}

func TestResamplerRampDownsample(t *testing.T) {
	r := newResampler(192000, 48000, 1, 2) // step 4
	out := r.process(rampS16(100))
	frames := len(out) / 2
	if frames < 20 {
		t.Fatalf("downsample frames = %d, want ~25", frames)
	}
	for i := 0; i < frames; i++ {
		want := float64(i) * 4
		if math.Abs(float64(readS16(out, i))-want) > 1 {
			t.Fatalf("frame %d = %d, want ~%.1f", i, readS16(out, i), want)
		}
	}
}

func TestResamplerContinuousAcrossChunks(t *testing.T) {
	// A linear ramp resampled in chunks must stay continuous: output frame i
	// tracks i*step regardless of chunk boundaries.
	r := newResampler(192000, 48000, 1, 2) // step 4
	var all []byte
	for start := 0; start < 200; start += 17 {
		end := start + 17
		if end > 200 {
			end = 200
		}
		chunk := make([]byte, (end-start)*2)
		for i := start; i < end; i++ {
			binary.LittleEndian.PutUint16(chunk[(i-start)*2:], uint16(int16(i)))
		}
		all = append(all, r.process(chunk)...)
	}
	frames := len(all) / 2
	if frames < 40 {
		t.Fatalf("continuous frames = %d", frames)
	}
	for i := 0; i < frames; i++ {
		want := float64(i) * 4
		if math.Abs(float64(readS16(all, i))-want) > 1 {
			t.Fatalf("discontinuity at frame %d = %d, want ~%.1f", i, readS16(all, i), want)
		}
	}
}

func TestResamplerS24(t *testing.T) {
	r := newResampler(48000, 96000, 1, 3) // step 0.5, 24-bit
	in := make([]byte, 40*3)
	for i := 0; i < 40; i++ {
		v := int32(i * 1000)
		in[i*3] = byte(v)
		in[i*3+1] = byte(v >> 8)
		in[i*3+2] = byte(v >> 16)
	}
	out := r.process(in)
	if len(out)/3 < 30 {
		t.Fatalf("s24 frames = %d", len(out)/3)
	}
	// frame 2 should be ~1000 (position 1.0)
	v := int32(out[2*3]) | int32(out[2*3+1])<<8 | int32(int8(out[2*3+2]))<<16
	if v < 900 || v > 1100 {
		t.Fatalf("s24 frame 2 = %d, want ~1000", v)
	}
}
