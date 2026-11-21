package decode

import (
	"bytes"
	"io"
	"testing"

	"hifihub/zat"
)

func TestOpenSegmentReadsSampleWindow(t *testing.T) {
	frames := 8
	pcm := make([]byte, 0, frames*4)
	for i := 0; i < frames; i++ {
		pcm = append(pcm, byte(i), 0, byte(255-i), 0)
	}
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)

	seg, err := OpenSegment(path, 2, 6)
	if err != nil {
		t.Fatalf("OpenSegment: %v", err)
	}
	defer seg.Close()
	if seg.TotalSamples() != 4 {
		t.Fatalf("TotalSamples = %d", seg.TotalSamples())
	}
	got, err := io.ReadAll(seg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pcm[2*4:6*4]) {
		t.Fatalf("segment payload = %x, want %x", got, pcm[2*4:6*4])
	}
}

func TestOpenSegmentStreamKeepsLocalClock(t *testing.T) {
	frames := 8
	pcm := make([]byte, frames*4)
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)
	seg, err := OpenSegment(path, 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()
	played, err := StreamFramesFrom(func(f zat.Frame) error { return nil }, seg, "trk-cue", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if played != 4 {
		t.Fatalf("played samples = %d", played)
	}
}

func TestOpenSegmentRejectsEmptyWindow(t *testing.T) {
	path := buildWAV(t, zat.S16LE, 44100, 2, make([]byte, 4*4))
	if _, err := OpenSegment(path, 4, 4); err == nil {
		t.Fatal("expected error for empty window")
	}
}
