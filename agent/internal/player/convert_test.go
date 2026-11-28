package player

import (
	"encoding/binary"
	"math"
	"testing"

	"hifihub/zat"
)

func TestPCMToFloat(t *testing.T) {
	cases := []struct {
		format zat.Format
		in     []byte
		want   float32
	}{
		{zat.S16LE, []byte{0x00, 0x00}, 0},
		{zat.S16LE, []byte{0x00, 0x80}, -1},
		{zat.S16LE, []byte{0xff, 0x7f}, 32767.0 / 32768},
		{zat.S24LE, []byte{0x00, 0x00, 0x00}, 0},
		{zat.S24LE, []byte{0x00, 0x00, 0x80}, -1},
		{zat.S24LE, []byte{0xff, 0xff, 0x7f}, 8388607.0 / 8388608},
		{zat.S32LE, []byte{0x00, 0x00, 0x00, 0x80}, -1},
		{zat.S32LE, []byte{0x00, 0x00, 0x00, 0x00}, 0},
	}
	for _, c := range cases {
		out := pcmToFloat(c.in, c.format)
		if len(out) != 4 {
			t.Fatalf("%s: got %d bytes, want 4", c.format, len(out))
		}
		got := math.Float32frombits(binary.LittleEndian.Uint32(out))
		if got != c.want {
			t.Errorf("%s %x: got %v, want %v", c.format, c.in, got, c.want)
		}
	}
	if out := pcmToFloat([]byte{0x01}, zat.S16LE); len(out) != 0 {
		t.Errorf("trailing partial sample must be dropped, got %d bytes", len(out))
	}
	if out := pcmToFloat([]byte{0x00, 0x00}, zat.Format("bogus")); out != nil {
		t.Errorf("unsupported format must return nil")
	}
}