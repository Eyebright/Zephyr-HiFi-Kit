//go:build !cgo || !linux

package player

import (
	"strings"
	"testing"

	"hifihub/zat"
)

func TestStubPlayerErrors(t *testing.T) {
	p := New()
	err := p.Open(zat.S16LE, 44100, 2, "null")
	if err == nil || !strings.Contains(err.Error(), "CGO") {
		t.Fatalf("stub Open should mention CGO backend, got %v", err)
	}
	if err := p.Write([]byte{0, 0, 0, 0}); err == nil {
		t.Fatal("stub Write should error")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("stub Close should be a no-op, got %v", err)
	}
}
