package decode

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"hifihub/zat"
)

// buildWAV writes a minimal PCM WAV file and returns its path.
func buildWAV(t *testing.T, format zat.Format, rate, channels int, pcm []byte) string {
	t.Helper()
	bits := format.BytesPerSample() * 8
	blockAlign := channels * bits / 8
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*blockAlign))
	binary.Write(&b, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return writeTemp(t, b.Bytes())
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

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

func TestWAVDecodeS16(t *testing.T) {
	pcm := rampPCM(1000, 2, 2)
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	f := d.Format()
	if f.Format != zat.S16LE || f.Rate != 44100 || f.Channels != 2 {
		t.Fatalf("format = %+v", f)
	}
	if d.TotalSamples() != 1000 {
		t.Fatalf("totalSamples = %d", d.TotalSamples())
	}
	got, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatal("decoded PCM does not match source")
	}
}

func TestWAVDecodeS24(t *testing.T) {
	pcm := rampPCM(500, 2, 3)
	path := buildWAV(t, zat.S24LE, 48000, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if d.Format().Format != zat.S24LE || d.TotalSamples() != 500 {
		t.Fatalf("format/total = %+v / %d", d.Format(), d.TotalSamples())
	}
	got, _ := io.ReadAll(d)
	if !bytes.Equal(got, pcm) {
		t.Fatal("S24 PCM mismatch")
	}
}

func TestWAVDecodeS32(t *testing.T) {
	pcm := rampPCM(256, 1, 4)
	path := buildWAV(t, zat.S32LE, 96000, 1, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if d.Format().Format != zat.S32LE || d.Format().Channels != 1 || d.TotalSamples() != 256 {
		t.Fatalf("format/total = %+v / %d", d.Format(), d.TotalSamples())
	}
	got, _ := io.ReadAll(d)
	if !bytes.Equal(got, pcm) {
		t.Fatal("S32 PCM mismatch")
	}
}

func TestWAVReadSmallBuffers(t *testing.T) {
	pcm := rampPCM(100, 2, 2)
	path := buildWAV(t, zat.S16LE, 44100, 2, pcm)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var got []byte
	buf := make([]byte, 7) // deliberately not frame-aligned
	for {
		n, err := d.Read(buf)
		got = append(got, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(got, pcm) {
		t.Fatal("small-buffer read mismatch")
	}
}

func TestWAVRejectFloat(t *testing.T) {
	// audioFormat 3 (IEEE float) is not supported in v0.1.
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(3)) // float
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint32(44100))
	binary.Write(&b, binary.LittleEndian, uint32(176400))
	binary.Write(&b, binary.LittleEndian, uint16(4))
	binary.Write(&b, binary.LittleEndian, uint16(32))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(0))
	if _, err := Open(writeTemp(t, b.Bytes())); err == nil {
		t.Fatal("expected float WAV rejection")
	}
}

func TestWAVExtensible(t *testing.T) {
	// WAVE_FORMAT_EXTENSIBLE (0xFFFE) with PCM subformat, 24-bit.
	pcm := rampPCM(64, 2, 3)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(60+len(pcm)))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(40))
	binary.Write(&b, binary.LittleEndian, uint16(0xFFFE))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint32(48000))
	binary.Write(&b, binary.LittleEndian, uint32(288000))
	binary.Write(&b, binary.LittleEndian, uint16(6))
	binary.Write(&b, binary.LittleEndian, uint16(24))
	binary.Write(&b, binary.LittleEndian, uint16(22)) // cbSize
	binary.Write(&b, binary.LittleEndian, uint16(24)) // valid bits
	binary.Write(&b, binary.LittleEndian, uint32(3))  // channel mask
	// SubFormat GUID: first 2 bytes = 1 (PCM), rest of the standard GUID.
	sub := []byte{1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xAA, 0, 0x38, 0x9B, 0x71}
	b.Write(sub)
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	d, err := Open(writeTemp(t, b.Bytes()))
	if err != nil {
		t.Fatalf("extensible open: %v", err)
	}
	defer d.Close()
	if d.Format().Format != zat.S24LE {
		t.Fatalf("extensible format = %+v", d.Format())
	}
	got, _ := io.ReadAll(d)
	if !bytes.Equal(got, pcm) {
		t.Fatal("extensible PCM mismatch")
	}
}
