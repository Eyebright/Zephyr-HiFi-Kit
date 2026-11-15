package zat

import (
	"encoding/json"
	"testing"
)

func TestControlFrameRoundtrip(t *testing.T) {
	h := Hello{NodeID: "n-1", SecretSHA256: "abcd", AgentVersion: "0.3.0",
		Caps: Caps{Codecs: []string{"pcm"}, Formats: []Format{S16LE, S24LE}, Rates: []int{44100, 96000}, MaxChannels: 2, ALSADevices: []string{"hw:0,0"}}}
	f, err := ControlFrame(TypeHello, 3, h)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeHello || f.Seq != 3 || f.PayloadLen != uint32(len(f.Payload)) {
		t.Fatalf("frame header mismatch: %+v", f.Header)
	}
	var got Hello
	if err := UnmarshalPayload(f.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.NodeID != "n-1" || got.Caps.MaxChannels != 2 || got.Caps.Formats[1] != S24LE {
		t.Fatalf("hello roundtrip mismatch: %+v", got)
	}
}

func TestSetupJSONContract(t *testing.T) {
	s := Setup{SessionID: "s-1", TrackID: "t-1", Format: S24LE, Rate: 96000, Channels: 2, PrebufferMS: 250, ALSADevice: "hw:1,0", TotalSamples: 12345678}
	b, err := MarshalPayload(s)
	if err != nil {
		t.Fatal(err)
	}
	// Field names are part of the wire contract; assert a few explicitly.
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"session_id", "track_id", "format", "rate", "channels", "prebuffer_ms", "alsa_device", "total_samples"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing wire field %q in %s", k, b)
		}
	}
	if m["format"] != "S24_LE" {
		t.Fatalf("format wire value = %v", m["format"])
	}
}

func TestStatusJSONContract(t *testing.T) {
	st := Status{SessionID: "s-1", State: "playing", PTS: 48000, BufferMS: 240, Underruns: 0, ALSADevice: "hw:1,0", DacHolderPID: 4242}
	f, err := ControlFrame(TypeStatus, 9, st)
	if err != nil {
		t.Fatal(err)
	}
	var got Status
	if err := UnmarshalPayload(f.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != "playing" || got.PTS != 48000 || got.DacHolderPID != 4242 {
		t.Fatalf("status roundtrip mismatch: %+v", got)
	}
}

func TestAudioFrameFlags(t *testing.T) {
	f := AudioFrame(5, AudioPayload{PTS: 0, TrackID: "t", PCM: []byte{0, 0}})
	if f.Type != TypeAudio || f.Flags&FlagAudio == 0 || f.Seq != 5 {
		t.Fatalf("audio frame flags mismatch: %+v", f.Header)
	}
	p, err := DecodeAudio(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if p.TrackID != "t" {
		t.Fatalf("track id = %q", p.TrackID)
	}
}

func TestTypeStrings(t *testing.T) {
	if TypeAudio.String() != "AUDIO" || TypeHello.String() != "HELLO" {
		t.Fatal("type string mismatch")
	}
	if Type(0xFF).String() != "UNKNOWN(0xff)" {
		t.Fatalf("unknown type string = %q", Type(0xFF).String())
	}
}
