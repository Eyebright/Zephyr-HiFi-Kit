package decode

import (
	"errors"
	"testing"
)

func TestOpenUnsupportedMP3(t *testing.T) {
	// MP3 is identified but not decodable in v0.1.
	path := writeTemp(t, []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0})
	_, err := Open(path)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestOpenUnknown(t *testing.T) {
	path := writeTemp(t, []byte{0, 1, 2, 3, 4, 5, 6, 7})
	if _, err := Open(path); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported for unknown, got %v", err)
	}
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := Open("/nonexistent/zephyr/audio.wav"); err == nil {
		t.Fatal("expected error for missing file")
	}
}
