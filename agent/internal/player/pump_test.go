package player

import (
	"bytes"
	"io"
	"testing"

	"hifihub/zat"
)

type fakePlayer struct {
	format         zat.Format
	rate, channels int
	device         string
	pcm            []byte
	writes         int
	drained        bool
	closed         bool
	writeErr       error
}

func (f *fakePlayer) Open(format zat.Format, rate, channels int, device string) error {
	f.format, f.rate, f.channels, f.device = format, rate, channels, device
	return nil
}
func (f *fakePlayer) Write(pcm []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.pcm = append(f.pcm, pcm...)
	f.writes++
	return nil
}
func (f *fakePlayer) Drain() error { f.drained = true; return nil }
func (f *fakePlayer) Close() error { f.closed = true; return nil }

func rampPCM(frames, channels, bps int) []byte {
	pcm := make([]byte, frames*channels*bps)
	for i := 0; i < frames*channels; i++ {
		v := int32((i*137)%65536) - 32768
		off := i * bps
		for k := 0; k < bps; k++ {
			pcm[off+k] = byte(v >> (8 * k))
		}
	}
	return pcm
}

// writeAudioFrames chunks pcm into ZAT AUDIO frames exactly as the server's
// decode.Stream does, so this test also proves wire-level interop.
func writeAudioFrames(t *testing.T, w io.Writer, pcm []byte, bpf int) uint32 {
	t.Helper()
	var seq uint32
	var pts uint64
	for off := 0; off < len(pcm); {
		end := off + zat.MaxAudioPCM
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[off:end]
		fr := zat.AudioFrame(seq, zat.AudioPayload{PTS: pts, TrackID: "trk-1", PCM: chunk})
		if err := zat.WriteFrame(w, fr); err != nil {
			t.Fatal(err)
		}
		seq++
		pts += uint64(len(chunk) / bpf)
		off = end
	}
	return seq
}

func TestPumpAudioRoundTrip(t *testing.T) {
	pcm := rampPCM(3000, 2, 2)
	var wire bytes.Buffer
	seq := writeAudioFrames(t, &wire, pcm, 4)
	stop, err := zat.ControlFrame(zat.TypeStop, seq, zat.Stop{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := zat.WriteFrame(&wire, stop); err != nil {
		t.Fatal(err)
	}

	fp := &fakePlayer{}
	last, err := PumpAudio(bytes.NewReader(wire.Bytes()), fp)
	if err != nil {
		t.Fatalf("pump: %v", err)
	}
	if last.Type != zat.TypeStop {
		t.Fatalf("expected STOP control frame, got %s", last.Type)
	}
	if !bytes.Equal(fp.pcm, pcm) {
		t.Fatal("played PCM != source")
	}
	if fp.writes < 1 {
		t.Fatal("player never written")
	}
}

func TestPumpAudioMultiChunk(t *testing.T) {
	pcm := rampPCM(40000, 2, 2) // > 64 KiB => multiple frames
	var wire bytes.Buffer
	writeAudioFrames(t, &wire, pcm, 4)
	fp := &fakePlayer{}
	if _, err := PumpAudio(bytes.NewReader(wire.Bytes()), fp); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if !bytes.Equal(fp.pcm, pcm) {
		t.Fatal("multi-chunk played PCM != source")
	}
	if fp.writes < 2 {
		t.Fatalf("expected multiple writes, got %d", fp.writes)
	}
}

func TestPumpAudioEOF(t *testing.T) {
	pcm := rampPCM(100, 2, 2)
	var wire bytes.Buffer
	writeAudioFrames(t, &wire, pcm, 4)
	fp := &fakePlayer{}
	if _, err := PumpAudio(bytes.NewReader(wire.Bytes()), fp); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if !bytes.Equal(fp.pcm, pcm) {
		t.Fatal("pcm mismatch")
	}
}

func TestPumpAudioWriteError(t *testing.T) {
	pcm := rampPCM(100, 2, 2)
	var wire bytes.Buffer
	writeAudioFrames(t, &wire, pcm, 4)
	fp := &fakePlayer{writeErr: io.ErrClosedPipe}
	if _, err := PumpAudio(bytes.NewReader(wire.Bytes()), fp); err == nil {
		t.Fatal("expected write error to propagate")
	}
}

func TestPumpAudioStopsAtControl(t *testing.T) {
	var wire bytes.Buffer
	// A PLAY frame before any audio must be returned immediately, untouched.
	play, err := zat.ControlFrame(zat.TypePlay, 0, zat.Play{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := zat.WriteFrame(&wire, play); err != nil {
		t.Fatal(err)
	}
	fp := &fakePlayer{}
	last, err := PumpAudio(bytes.NewReader(wire.Bytes()), fp)
	if err != nil {
		t.Fatalf("pump: %v", err)
	}
	if last.Type != zat.TypePlay {
		t.Fatalf("expected PLAY, got %s", last.Type)
	}
	if fp.writes != 0 {
		t.Fatal("player written before any audio")
	}
}
