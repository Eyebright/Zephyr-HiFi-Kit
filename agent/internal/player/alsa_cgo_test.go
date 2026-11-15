//go:build cgo && linux

package player

import (
	"testing"

	"hifihub/zat"
)

// TestALSANullLoopback exercises the real ALSA bindings against the "null" PCM,
// which needs no sound hardware. It runs in Linux CI (CGO + libasound2-dev).
func TestALSANullLoopback(t *testing.T) {
	for _, format := range []zat.Format{zat.S16LE, zat.S24LE, zat.S32LE} {
		t.Run(string(format), func(t *testing.T) {
			p := New()
			if err := p.Open(format, 44100, 2, "null"); err != nil {
				t.Fatalf("open null (%s): %v", format, err)
			}
			defer p.Close()
			bpf := format.BytesPerSample() * 2
			pcm := make([]byte, 4410*bpf) // 100 ms of silence
			if err := p.Write(pcm); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := p.Drain(); err != nil {
				t.Fatalf("drain: %v", err)
			}
		})
	}
}

func TestALSAOpenBadDevice(t *testing.T) {
	p := New()
	if err := p.Open(zat.S16LE, 44100, 2, "zephyr-no-such-device"); err == nil {
		p.Close()
		t.Fatal("expected error opening a nonexistent device")
	}
}
