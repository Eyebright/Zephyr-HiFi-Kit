package player

import (
	"bytes"
	"testing"
)

func TestPack16to32(t *testing.T) {
	in := []byte{0x34, 0x12, 0xCD, 0xAB}
	got := pack16to32(in)
	want := []byte{0, 0, 0x34, 0x12, 0, 0, 0xCD, 0xAB}
	if !bytes.Equal(got, want) {
		t.Fatalf("packed = % x want % x", got, want)
	}
}

func TestPack24to32(t *testing.T) {
	in := []byte{0x01, 0x02, 0x03, 0xFF, 0x00, 0x80}
	got := pack24to32(in)
	want := []byte{0, 0x01, 0x02, 0x03, 0, 0xFF, 0x00, 0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("packed = % x want % x", got, want)
	}
}
