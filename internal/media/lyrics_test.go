package media

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEmbeddedLyricsFLAC(t *testing.T) {
	p := writeFLAC(t, flacHead()[4:], flacBlock(4, true, vorbisComments(
		"TITLE=歌曲", "LYRICS=[00:01.00]你好\n[00:03.50]世界")))
	got, err := ReadEmbeddedLyrics(p, "flac", EncodingAuto)
	if err != nil || !strings.Contains(got, "[00:01.00]你好") {
		t.Fatalf("FLAC lyrics = %q, %v", got, err)
	}
}

func TestReadEmbeddedLyricsID3(t *testing.T) {
	uslt := append([]byte{3, 'e', 'n', 'g', 0}, []byte("[00:02.00]世界")...)
	p := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(p, id3Tag(3, 0, id3Frame(3, "USLT", uslt)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEmbeddedLyrics(p, "mp3", EncodingAuto)
	if err != nil || got != "[00:02.00]世界" {
		t.Fatalf("USLT lyrics = %q, %v", got, err)
	}

	var sylt []byte
	sylt = append(sylt, 3, 'e', 'n', 'g', 2, 1, 0)
	sylt = append(sylt, []byte("你好\x00")...)
	sylt = binary.BigEndian.AppendUint32(sylt, 1500)
	sylt = append(sylt, []byte("世界\x00")...)
	sylt = binary.BigEndian.AppendUint32(sylt, 3000)
	p = filepath.Join(t.TempDir(), "sync.mp3")
	if err := os.WriteFile(p, id3Tag(3, 0, id3Frame(3, "SYLT", sylt)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = ReadEmbeddedLyrics(p, "mp3", EncodingAuto)
	if err != nil || !strings.Contains(got, "[00:01.500]你好") || !strings.Contains(got, "[00:03.000]世界") {
		t.Fatalf("SYLT lyrics = %q, %v", got, err)
	}
}

func TestReadSidecarLyrics(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "Song.flac")
	if err := os.WriteFile(audio, []byte("fLaC"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSidecarLyrics(audio, EncodingAuto); !errors.Is(err, ErrNoLyrics) {
		t.Fatalf("missing sidecar error = %v", err)
	}
	lrc := filepath.Join(dir, "song.LRC")
	if err := os.WriteFile(lrc, []byte("\ufeff[00:01.00]你好"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSidecarLyrics(audio, EncodingAuto)
	if err != nil || got != "[00:01.00]你好" {
		t.Fatalf("sidecar lyrics = %q, %v", got, err)
	}
}
