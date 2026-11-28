package decode

import (
	"bytes"
	"io"
	"testing"

	"hifihub/zat"
)

// streamAndCollect runs Stream over a WAV and returns the reassembled PCM plus
// the per-frame PTS values, proving the server half of the vertical slice.
func streamAndCollect(t *testing.T, pcm []byte, format zat.Format, rate, channels int) ([]byte, []uint64, uint64) {
	t.Helper()
	path := buildWAV(t, format, rate, channels, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	var wire bytes.Buffer
	total, err := Stream(&wire, d, "trk-1", 0)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var (
		got []byte
		pts []uint64
		seq uint32
	)
	r := bytes.NewReader(wire.Bytes())
	for {
		fr, err := zat.ReadFrame(r)
		if err == zat.ErrShortHeader {
			break // clean end of stream
		}
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if fr.Type != zat.TypeAudio {
			t.Fatalf("expected AUDIO frame, got %s", fr.Type)
		}
		if fr.Seq != seq {
			t.Fatalf("seq = %d want %d", fr.Seq, seq)
		}
		seq++
		ap, err := zat.DecodeAudio(fr.Payload)
		if err != nil {
			t.Fatalf("decode audio: %v", err)
		}
		if ap.TrackID != "trk-1" {
			t.Fatalf("trackID = %q", ap.TrackID)
		}
		if len(ap.PCM) > zat.MaxAudioPCM {
			t.Fatalf("audio frame exceeds cap: %d", len(ap.PCM))
		}
		bpf := format.BytesPerSample() * channels
		if len(ap.PCM)%bpf != 0 {
			t.Fatalf("audio frame not whole frames: %d %% %d", len(ap.PCM), bpf)
		}
		pts = append(pts, ap.PTS)
		got = append(got, ap.PCM...)
	}
	return got, pts, total
}

func TestStreamRoundTripSmall(t *testing.T) {
	pcm := rampPCM(1000, 2, 2)
	got, pts, total := streamAndCollect(t, pcm, zat.S16LE, 44100, 2)
	if !bytes.Equal(got, pcm) {
		t.Fatal("reassembled PCM != source")
	}
	if total != 1000 {
		t.Fatalf("total samples = %d want 1000", total)
	}
	if len(pts) != 1 || pts[0] != 0 {
		t.Fatalf("expected one frame at PTS 0, got %v", pts)
	}
}

func TestStreamRoundTripMultiChunk(t *testing.T) {
	// 40000 stereo S16 frames = 160000 bytes > 64 KiB cap => multiple frames.
	frames := 40000
	pcm := rampPCM(frames, 2, 2)
	got, pts, total := streamAndCollect(t, pcm, zat.S16LE, 44100, 2)
	if !bytes.Equal(got, pcm) {
		t.Fatal("multi-chunk reassembled PCM != source")
	}
	if total != uint64(frames) {
		t.Fatalf("total = %d want %d", total, frames)
	}
	if len(pts) < 2 {
		t.Fatalf("expected multiple audio frames, got %d", len(pts))
	}
	// PTS must be a monotonically increasing sample clock.
	bpf := 4
	framesPerChunk := zat.MaxAudioPCM / bpf
	for i, p := range pts {
		want := uint64(i * framesPerChunk)
		if p != want {
			t.Fatalf("pts[%d] = %d want %d", i, p, want)
		}
	}
}

func TestStreamS24RoundTrip(t *testing.T) {
	pcm := rampPCM(2000, 2, 3)
	got, _, total := streamAndCollect(t, pcm, zat.S24LE, 48000, 2)
	if !bytes.Equal(got, pcm) {
		t.Fatal("S24 reassembled PCM != source")
	}
	if total != 2000 {
		t.Fatalf("total = %d want 2000", total)
	}
}

func TestStreamS24MultiChunk(t *testing.T) {
	// 6-byte frames do not divide the 64 KiB read buffer: every chunk ends with
	// a partial frame that must be carried into the next read, not dropped.
	frames := 40000
	pcm := rampPCM(frames, 2, 3)
	got, pts, total := streamAndCollect(t, pcm, zat.S24LE, 48000, 2)
	if !bytes.Equal(got, pcm) {
		t.Fatal("multi-chunk S24 reassembled PCM != source (partial frames dropped)")
	}
	if total != uint64(frames) {
		t.Fatalf("total = %d want %d", total, frames)
	}
	bpf := 6
	framesPerChunk := zat.MaxAudioPCM / bpf
	for i, p := range pts {
		if want := uint64(i * framesPerChunk); p != want {
			t.Fatalf("pts[%d] = %d want %d", i, p, want)
		}
	}
}

func TestStreamS24MultiChunkOffset(t *testing.T) {
	frames := 40000
	pcm := rampPCM(frames, 2, 3)
	path := buildWAV(t, zat.S24LE, 48000, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	const start = 20001 // mid-chunk, mid-partial-frame offset
	var got []byte
	sink := func(f zat.Frame) error {
		ap, err := zat.DecodeAudio(f.Payload)
		if err != nil {
			return err
		}
		got = append(got, ap.PCM...)
		return nil
	}
	end, err := StreamFramesFrom(sink, d, "trk-1", 0, start)
	if err != nil {
		t.Fatal(err)
	}
	if end != uint64(frames) {
		t.Fatalf("end = %d want %d", end, frames)
	}
	if !bytes.Equal(got, pcm[start*6:]) {
		t.Fatalf("offset S24 PCM mismatch: got %d bytes want %d", len(got), len(pcm)-start*6)
	}
}

// oddReader returns PCM in reads that never align to frame boundaries, like a
// pipe-backed decoder (ffmpeg DSD fallback).
type oddReader struct {
	pcm  []byte
	off  int
	step int
}

func (r *oddReader) Read(p []byte) (int, error) {
	if r.off >= len(r.pcm) {
		return 0, io.EOF
	}
	n := r.step
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.pcm)-r.off {
		n = len(r.pcm) - r.off
	}
	copy(p, r.pcm[r.off:r.off+n])
	r.off += n
	return n, nil
}

type oddDecoder struct {
	oddReader
	format PCMFormat
}

func (d *oddDecoder) Format() PCMFormat    { return d.format }
func (d *oddDecoder) TotalSamples() uint64 { return uint64(len(d.pcm) / 6) }
func (d *oddDecoder) Close() error         { return nil }

func TestStreamCarriesPartialFrames(t *testing.T) {
	pcm := rampPCM(30000, 2, 3)
	d := &oddDecoder{oddReader: oddReader{pcm: pcm, step: 1001}, format: PCMFormat{Format: zat.S24LE, Rate: 48000, Channels: 2}}
	var got []byte
	sink := func(f zat.Frame) error {
		ap, err := zat.DecodeAudio(f.Payload)
		if err != nil {
			return err
		}
		got = append(got, ap.PCM...)
		return nil
	}
	if _, err := StreamFramesFrom(sink, d, "trk-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatalf("unaligned reads lost bytes: got %d want %d", len(got), len(pcm))
	}
}

func TestStreamWriterError(t *testing.T) {
	pcm := rampPCM(100, 2, 2)
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Stream(failWriter{}, d, "t", 0); err == nil {
		t.Fatal("expected write error to propagate")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestStreamFramesFromOffset(t *testing.T) {
	pcm := rampPCM(40000, 2, 2) // 160000 bytes => multiple 64KiB chunks
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	const start = 20000 // mid-chunk offset: exercises the trim path
	var got []byte
	var firstPTS uint64
	var frames int
	sink := func(f zat.Frame) error {
		ap, err := zat.DecodeAudio(f.Payload)
		if err != nil {
			return err
		}
		if frames == 0 {
			firstPTS = ap.PTS
		}
		frames++
		got = append(got, ap.PCM...)
		return nil
	}
	end, err := StreamFramesFrom(sink, d, "trk-1", 0, start)
	if err != nil {
		t.Fatal(err)
	}
	if firstPTS != start {
		t.Fatalf("first PTS = %d want %d", firstPTS, start)
	}
	if end != 40000 {
		t.Fatalf("end = %d want 40000", end)
	}
	if !bytes.Equal(got, pcm[start*4:]) {
		t.Fatalf("offset PCM mismatch: got %d bytes want %d", len(got), len(pcm)-start*4)
	}
}
