package zat

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestHeaderByteLayout(t *testing.T) {
	var b [HeaderSize]byte
	EncodeHeader(b[:], Header{Version: Version, Type: TypeAudio, Flags: FlagAudio | FlagNeedAck, Seq: 0x01020304, PayloadLen: 0x000A0B0C})
	if string(b[0:4]) != Magic {
		t.Fatalf("magic = %q", b[0:4])
	}
	if b[4] != Version || b[5] != byte(TypeAudio) {
		t.Fatalf("version/type = %d/%d", b[4], b[5])
	}
	if got := binary.BigEndian.Uint16(b[6:8]); got != uint16(FlagAudio|FlagNeedAck) {
		t.Fatalf("flags = %d", got)
	}
	if got := binary.BigEndian.Uint32(b[8:12]); got != 0x01020304 {
		t.Fatalf("seq = %#x", got)
	}
	if got := binary.BigEndian.Uint32(b[12:16]); got != 0x000A0B0C {
		t.Fatalf("payloadlen = %#x", got)
	}
	h, err := DecodeHeader(b[:])
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != TypeAudio || h.Seq != 0x01020304 || h.PayloadLen != 0x000A0B0C || h.Flags != FlagAudio|FlagNeedAck {
		t.Fatalf("roundtrip mismatch: %+v", h)
	}
}

func TestDecodeHeaderRejects(t *testing.T) {
	var b [HeaderSize]byte
	EncodeHeader(b[:], Header{Version: Version, Type: TypePing})

	bad := b
	bad[0] = 'X'
	if _, err := DecodeHeader(bad[:]); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("want ErrBadMagic, got %v", err)
	}
	bad = b
	bad[4] = 0x99
	if _, err := DecodeHeader(bad[:]); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("want ErrBadVersion, got %v", err)
	}
	bad = b
	binary.BigEndian.PutUint32(bad[12:16], MaxPayload+1)
	if _, err := DecodeHeader(bad[:]); !errors.Is(err, ErrPayloadTooBig) {
		t.Fatalf("want ErrPayloadTooBig, got %v", err)
	}
	if _, err := DecodeHeader(b[:8]); !errors.Is(err, ErrShortHeader) {
		t.Fatalf("want ErrShortHeader, got %v", err)
	}
}

func TestReadWriteFrameRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	in := Frame{Header: Header{Type: TypeStatus, Seq: 7}, Payload: []byte(`{"state":"playing"}`)}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != TypeStatus || out.Seq != 7 || string(out.Payload) != `{"state":"playing"}` {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
	if out.Version != Version {
		t.Fatalf("version not stamped: %d", out.Version)
	}
}

func TestReadFrameShort(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader([]byte("ZAT"))); !errors.Is(err, ErrShortHeader) {
		t.Fatalf("want ErrShortHeader, got %v", err)
	}
	var buf bytes.Buffer
	_ = WriteFrame(&buf, Frame{Header: Header{Type: TypePing}, Payload: []byte("hello")})
	trunc := buf.Bytes()[:HeaderSize+2]
	if _, err := ReadFrame(bytes.NewReader(trunc)); !errors.Is(err, ErrTruncated) {
		t.Fatalf("want ErrTruncated, got %v", err)
	}
}

func TestReadFrameEOF(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader(nil)); !errors.Is(err, ErrShortHeader) {
		t.Fatalf("want ErrShortHeader on empty, got %v", err)
	}
}

func TestWriteFrameCap(t *testing.T) {
	err := WriteFrame(io.Discard, Frame{Payload: make([]byte, MaxPayload+1)})
	if !errors.Is(err, ErrPayloadTooBig) {
		t.Fatalf("want ErrPayloadTooBig, got %v", err)
	}
}

func TestAudioPayloadRoundtrip(t *testing.T) {
	pcm := []byte{1, 2, 3, 4, 5, 6}
	in := AudioPayload{PTS: 48000, TrackID: "t-1", PCM: pcm}
	out, err := DecodeAudio(EncodeAudio(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.PTS != 48000 || out.TrackID != "t-1" || !bytes.Equal(out.PCM, pcm) {
		t.Fatalf("audio roundtrip mismatch: %+v", out)
	}
}

func TestDecodeAudioTruncated(t *testing.T) {
	if _, err := DecodeAudio([]byte{1, 2, 3}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("want ErrTruncated, got %v", err)
	}
	b := EncodeAudio(AudioPayload{TrackID: "abc", PCM: []byte{9}})
	if _, err := DecodeAudio(b[:13]); !errors.Is(err, ErrTruncated) {
		t.Fatalf("want ErrTruncated on short track id, got %v", err)
	}
}

func TestFormatHelpers(t *testing.T) {
	cases := map[Format]int{S16LE: 2, S24LE: 3, S32LE: 4, Format("bogus"): 0}
	for f, want := range cases {
		if got := f.BytesPerSample(); got != want {
			t.Fatalf("%s bps = %d want %d", f, got, want)
		}
	}
	if !S24LE.Valid() || Format("bogus").Valid() {
		t.Fatal("Valid mismatch")
	}
	// 2ch S24_LE: 6 bytes per sample frame → 60 bytes = 10 frames.
	if got := FrameSamples(60, 2, S24LE); got != 10 {
		t.Fatalf("FrameSamples = %d want 10", got)
	}
	if got := FrameSamples(60, 0, S16LE); got != 0 {
		t.Fatalf("FrameSamples zero channels = %d", got)
	}
}
