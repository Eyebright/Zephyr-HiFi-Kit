package decode

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"hifihub/zat"
)

func writeSineWAV(t *testing.T, path string, bits, rate, channels int, frames int) []byte {
	t.Helper()
	bps := bits / 8
	data := make([]byte, frames*channels*bps)
	for i := 0; i < frames; i++ {
		v := 0.4 * math.Sin(2*math.Pi*997*float64(i)/float64(rate))
		for ch := 0; ch < channels; ch++ {
			off := (i*channels + ch) * bps
			switch bits {
			case 16:
				binary.LittleEndian.PutUint16(data[off:], uint16(int16(math.Round(v*32768))))
			case 24:
				u := uint32(int32(math.Round(v * 8388608)))
				data[off], data[off+1], data[off+2] = byte(u), byte(u>>8), byte(u>>16)
			case 32:
				binary.LittleEndian.PutUint32(data[off:], uint32(int32(math.Round(v*2147483648))))
			}
		}
	}
	var hdr bytes.Buffer
	hdr.WriteString("RIFF")
	binary.Write(&hdr, binary.LittleEndian, uint32(36+len(data)))
	hdr.WriteString("WAVE")
	hdr.WriteString("fmt ")
	binary.Write(&hdr, binary.LittleEndian, uint32(16))
	binary.Write(&hdr, binary.LittleEndian, uint16(1))
	binary.Write(&hdr, binary.LittleEndian, uint16(channels))
	binary.Write(&hdr, binary.LittleEndian, uint32(rate))
	binary.Write(&hdr, binary.LittleEndian, uint32(rate*channels*bps))
	binary.Write(&hdr, binary.LittleEndian, uint16(channels*bps))
	binary.Write(&hdr, binary.LittleEndian, uint16(bits))
	hdr.WriteString("data")
	binary.Write(&hdr, binary.LittleEndian, uint32(len(data)))
	hdr.Write(data)
	if err := os.WriteFile(path, hdr.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWAVLoopbackBitExact(t *testing.T) {
	for _, tc := range []struct{ bits, rate int }{{16, 44100}, {24, 96000}, {24, 192000}, {32, 384000}} {
		dir := t.TempDir()
		path := filepath.Join(dir, "sine.wav")
		want := writeSineWAV(t, path, tc.bits, tc.rate, 2, 4096)
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var got []byte
		sink := func(f zat.Frame) error {
			p, err := zat.DecodeAudio(f.Payload)
			if err != nil {
				return err
			}
			got = append(got, p.PCM...)
			return nil
		}
		if _, err := StreamFramesFrom(sink, d, "t", 0, 0); err != nil {
			t.Fatal(err)
		}
		d.Close()
		if !bytes.Equal(got, want) {
			t.Fatalf("bits=%d rate=%d loopback not bit-exact (got %d bytes want %d)", tc.bits, tc.rate, len(got), len(want))
		}
	}
}