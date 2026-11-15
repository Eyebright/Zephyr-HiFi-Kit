//go:build cgo && android

package player

import (
	"testing"

	"hifihub/zat"
)

// TestAAudioLoopback exercises the real AAudio bindings against the default
// output. It runs on an Android emulator/device in CI (CGO + NDK, API 26+).
// Exclusive sharing may be unavailable on the emulator; Open falls back to
// shared, so the test still validates the full open/write/drain/close path.
func TestAAudioLoopback(t *testing.T) {
	for _, format := range []zat.Format{zat.S16LE, zat.S24LE, zat.S32LE} {
		t.Run(string(format), func(t *testing.T) {
			p := New()
			if err := p.Open(format, 48000, 2, "default"); err != nil {
				t.Fatalf("open AAudio (%s): %v", format, err)
			}
			defer p.Close()
			bpf := format.BytesPerSample() * 2
			pcm := make([]byte, 4800*bpf) // 100 ms of silence
			if err := p.Write(pcm); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := p.Drain(); err != nil {
				t.Fatalf("drain: %v", err)
			}
		})
	}
}

func TestAAudioOpenBadFormat(t *testing.T) {
	p := New()
	if err := p.Open(zat.Format("bogus"), 48000, 2, "default"); err == nil {
		p.Close()
		t.Fatal("expected error opening an unsupported format")
	}
}
