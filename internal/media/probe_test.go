package media

import (
	"encoding/binary"
	"testing"
)

func flacHead() []byte {
	h := make([]byte, 42)
	copy(h[0:4], "fLaC")
	h[4] = 0x00                         // STREAMINFO, not last
	h[5], h[6], h[7] = 0x00, 0x00, 0x22 // length 34
	// packed: rate=44100, channels=2, bits=16, totalSamples=1000000
	h[18], h[19], h[20] = 0x0A, 0xC4, 0x42
	h[21], h[22], h[23], h[24], h[25] = 0xF0, 0x00, 0x0F, 0x42, 0x40
	return h
}

func TestProbeFLAC(t *testing.T) {
	r := Probe(flacHead(), 1<<20)
	if r.Codec != "flac" || !r.Decodable {
		t.Fatalf("codec/decodable = %s/%v", r.Codec, r.Decodable)
	}
	if r.Rate != 44100 || r.Channels != 2 || r.Bits != 16 {
		t.Fatalf("rate/ch/bits = %d/%d/%d", r.Rate, r.Channels, r.Bits)
	}
	if r.TotalSamples != 1000000 {
		t.Fatalf("totalSamples = %d", r.TotalSamples)
	}
	if r.DurationMS != 22675 { // 1000000*1000/44100
		t.Fatalf("duration = %d", r.DurationMS)
	}
}

func TestProbeWAV(t *testing.T) {
	h := make([]byte, 0, 64)
	h = append(h, "RIFF"...)
	h = append(h, 0, 0, 0, 0) // riff size (unused)
	h = append(h, "WAVE"...)
	h = append(h, "fmt "...)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], 16)
	h = append(h, sz[:]...)
	var f [16]byte
	binary.LittleEndian.PutUint16(f[0:2], 1)     // PCM
	binary.LittleEndian.PutUint16(f[2:4], 2)     // channels
	binary.LittleEndian.PutUint32(f[4:8], 48000) // rate
	binary.LittleEndian.PutUint16(f[14:16], 24)  // bits
	h = append(h, f[:]...)
	h = append(h, "data"...)
	binary.LittleEndian.PutUint32(sz[:], 480000)
	h = append(h, sz[:]...)

	r := Probe(h, int64(len(h)))
	if r.Codec != "pcm_s24le" || !r.Decodable {
		t.Fatalf("codec = %s", r.Codec)
	}
	if r.Rate != 48000 || r.Channels != 2 || r.Bits != 24 {
		t.Fatalf("rate/ch/bits = %d/%d/%d", r.Rate, r.Channels, r.Bits)
	}
	if r.TotalSamples != 80000 { // 480000 / (2*3)
		t.Fatalf("totalSamples = %d", r.TotalSamples)
	}
	if r.DurationMS != 1666 {
		t.Fatalf("duration = %d", r.DurationMS)
	}
}

func TestProbeMP3Xing(t *testing.T) {
	h := make([]byte, 64)
	h[0], h[1], h[2], h[3] = 0xFF, 0xFB, 0x90, 0x00 // MPEG1 Layer3 44100 128k stereo
	copy(h[36:40], "Xing")
	binary.BigEndian.PutUint32(h[40:44], 0x1) // frames flag
	binary.BigEndian.PutUint32(h[44:48], 1000)
	r := Probe(h, 1<<20)
	if r.Codec != "mp3" || r.Rate != 44100 || r.Channels != 2 {
		t.Fatalf("mp3 = %+v", r)
	}
	if r.TotalSamples != 1152000 { // 1000 * 1152
		t.Fatalf("totalSamples = %d", r.TotalSamples)
	}
}

func TestProbeMP3CBR(t *testing.T) {
	h := make([]byte, 8)
	h[0], h[1], h[2], h[3] = 0xFF, 0xFB, 0x90, 0x00
	// 128 kbps, ~1,000,000 bytes → ~62500 ms
	r := Probe(h, 1000000)
	if r.Rate != 44100 || r.Channels != 2 {
		t.Fatalf("mp3 cbr params = %+v", r)
	}
	if r.DurationMS < 60000 || r.DurationMS > 65000 {
		t.Fatalf("cbr duration = %d", r.DurationMS)
	}
}

func TestProbeMP3ID3Skip(t *testing.T) {
	h := make([]byte, 128)
	copy(h[0:3], "ID3")
	h[3], h[4] = 3, 0
	h[5] = 0
	// syncsafe size = 20 → tag occupies 10+20 = 30 bytes
	h[6], h[7], h[8], h[9] = 0, 0, 0, 20
	h[30], h[31], h[32], h[33] = 0xFF, 0xFB, 0x90, 0x00
	r := Probe(h, 1<<20)
	if r.Rate != 44100 || r.Channels != 2 {
		t.Fatalf("id3 skip failed: %+v", r)
	}
}

func TestProbeOGGVorbis(t *testing.T) {
	h := make([]byte, 64)
	copy(h[0:4], "OggS")
	h[28] = 0x01
	copy(h[29:35], "vorbis")
	h[39] = 2 // channels at i+11
	binary.LittleEndian.PutUint32(h[40:44], 48000)
	r := Probe(h, 1<<20)
	if r.Codec != "vorbis" || r.Channels != 2 || r.Rate != 48000 {
		t.Fatalf("ogg = %+v", r)
	}
	if r.Decodable {
		t.Fatal("vorbis should not be decodable in v0.1")
	}
}

func TestProbeADTS(t *testing.T) {
	h := []byte{0xFF, 0xF1, 0x10, 0x80}
	r := Probe(h, 1<<20)
	if r.Codec != "aac" || r.Rate != 44100 || r.Channels != 2 {
		t.Fatalf("adts = %+v", r)
	}
}

func TestProbeDSF(t *testing.T) {
	h := make([]byte, 92)
	copy(h[0:4], "DSD ")
	copy(h[28:32], "fmt ")
	copy(h[44:48], "DSD ") // format id 0 is stored little-endian
	binary.LittleEndian.PutUint32(h[52:56], 2)
	binary.LittleEndian.PutUint32(h[56:60], 2822400)
	binary.LittleEndian.PutUint32(h[60:64], 1)
	binary.LittleEndian.PutUint64(h[64:72], 2822400*10)
	binary.LittleEndian.PutUint32(h[72:76], 4096)
	copy(h[80:84], "data")
	r := Probe(h, 1<<20)
	if r.Codec != "dsd" || r.Container != "dsf" || r.Channels != 2 || r.Rate != 2822400 {
		t.Fatalf("dsf = %+v", r)
	}
	if r.TotalSamples != 2822400*10/16 || r.DurationMS != 10000 || !r.Decodable {
		t.Fatalf("dsf totals = %+v", r)
	}
}

func TestProbeDFF(t *testing.T) {
	h := make([]byte, 96)
	copy(h[0:4], "FRM8")
	copy(h[12:16], "DSD ")
	copy(h[16:20], "PROP")
	binary.BigEndian.PutUint64(h[20:28], 34)
	copy(h[28:32], "SND ")
	copy(h[32:36], "FS  ")
	binary.BigEndian.PutUint64(h[36:44], 4)
	binary.BigEndian.PutUint32(h[44:48], 2822400)
	copy(h[48:52], "CHNL")
	binary.BigEndian.PutUint64(h[52:60], 2)
	binary.BigEndian.PutUint16(h[60:62], 2)
	copy(h[62:66], "DSD ")
	binary.BigEndian.PutUint64(h[66:74], 705600)
	r := Probe(h, 1<<20)
	if r.Codec != "dsd" || r.Container != "dff" || r.Channels != 2 || r.Rate != 2822400 || !r.Decodable {
		t.Fatalf("dff = %+v", r)
	}
}

func TestProbeMP4ALAC(t *testing.T) {
	h := make([]byte, 64)
	copy(h[4:8], "ftyp")
	copy(h[32:36], "alac")
	r := Probe(h, 1<<20)
	if r.Codec != "alac" || r.Container != "m4a" {
		t.Fatalf("m4a = %+v", r)
	}
}

func TestProbeUnknown(t *testing.T) {
	if r := Probe([]byte{0, 1, 2, 3}, 10); r.Codec != "unknown" {
		t.Fatalf("unknown = %+v", r)
	}
	if r := Probe([]byte{1}, 10); r.Codec != "unknown" {
		t.Fatalf("short = %+v", r)
	}
}

func TestProbeFLACMQAApplicationBlock(t *testing.T) {
	h := []byte("fLaC")
	si := make([]byte, 4+34)
	si[0], si[1], si[2], si[3] = 0x00, 0x00, 0x00, 0x22 // STREAMINFO, 34 bytes
	h = append(h, si...)
	app := []byte{0x02, 0x00, 0x00, 0x04, 'M', 'Q', 'A', ' '}
	h = append(h, app...)
	if r := Probe(h, int64(len(h))); !r.MQA {
		t.Fatalf("MQA application block not detected: %+v", r)
	}
}

func TestProbeFLACMQATag(t *testing.T) {
	h := []byte("fLaC")
	si := make([]byte, 4+34)
	si[3] = 0x22
	h = append(h, si...)
	comment := []byte("MQAENCODER=MQAEncode v1.2")
	vendor := []byte("reference libFLAC")
	payload := make([]byte, 0, 4+len(vendor)+4+4+len(comment))
	payload = appendLE(payload, len(vendor))
	payload = append(payload, vendor...)
	payload = appendLE(payload, 1)
	payload = appendLE(payload, len(comment))
	payload = append(payload, comment...)
	block := []byte{0x84, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	h = append(h, block...)
	h = append(h, payload...)
	if r := Probe(h, int64(len(h))); !r.MQA {
		t.Fatalf("MQA tag not detected: %+v", r)
	}
}

func appendLE(b []byte, v int) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
