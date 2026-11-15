//go:build !cgo || !linux

package decode

import (
	"strings"
	"testing"
)

func TestOpenFLACStub(t *testing.T) {
	h := make([]byte, 42)
	copy(h[0:4], "fLaC")
	h[4] = 0x00 // STREAMINFO
	h[5], h[6], h[7] = 0x00, 0x00, 0x22
	h[18], h[19], h[20] = 0x0A, 0xC4, 0x42 // rate 44100, ch 2, bits 16
	_, err := Open(writeTemp(t, h))
	if err == nil {
		t.Fatal("expected FLAC stub error on non-CGO build")
	}
	if !strings.Contains(err.Error(), "CGO") {
		t.Fatalf("stub error should mention CGO backend: %v", err)
	}
}
