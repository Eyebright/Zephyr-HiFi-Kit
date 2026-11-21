package decode

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"hifihub/server/internal/media"
	"hifihub/zat"
)

func TestDSFDoPPacking(t *testing.T) {
	h := make([]byte, 92)
	copy(h[0:4], "DSD ")
	copy(h[28:32], "fmt ")
	binary.LittleEndian.PutUint64(h[32:40], 52)
	binary.LittleEndian.PutUint32(h[40:44], 1)
	binary.LittleEndian.PutUint32(h[48:52], 2)
	binary.LittleEndian.PutUint32(h[52:56], 2)
	binary.LittleEndian.PutUint32(h[56:60], 2822400)
	binary.LittleEndian.PutUint32(h[60:64], 8)
	binary.LittleEndian.PutUint64(h[64:72], 32)
	binary.LittleEndian.PutUint32(h[72:76], 4)
	copy(h[80:84], "data")
	binary.LittleEndian.PutUint64(h[84:92], 8)
	h = append(h, 1, 2, 3, 4, 5, 6, 7, 8)
	path := filepath.Join(t.TempDir(), "x.dsf")
	if err := os.WriteFile(path, h, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := openDSD(path, media.ProbeResult{Codec: "dsd", Container: "dsf", Channels: 2, Rate: 2822400})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if f := d.Format(); f.Format != zat.S24LE || f.Rate != 176400 || f.Channels != 2 {
		t.Fatalf("format = %+v", f)
	}
	if d.TotalSamples() != 2 {
		t.Fatalf("frames = %d", d.TotalSamples())
	}
	out := make([]byte, 12)
	if _, err := d.Read(out); err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 2, 0x05, 5, 6, 0x05, 3, 4, 0xFA, 7, 8, 0xFA}
	if !bytes.Equal(out, want) {
		t.Fatalf("dop = % x want % x", out, want)
	}
}

func TestDFFDoPPackingAndSeek(t *testing.T) {
	h := make([]byte, 82)
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
	binary.BigEndian.PutUint64(h[66:74], 8)
	copy(h[74:82], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	path := filepath.Join(t.TempDir(), "x.dff")
	if err := os.WriteFile(path, h, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := openDSD(path, media.ProbeResult{Codec: "dsd", Container: "dff", Channels: 2, Rate: 2822400})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out := make([]byte, 12)
	if _, err := d.Read(out); err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 3, 0x05, 2, 4, 0x05, 5, 7, 0xFA, 6, 8, 0xFA}
	if !bytes.Equal(out, want) {
		t.Fatalf("dop = % x want % x", out, want)
	}
	sk, ok := d.(interface{ SeekSample(uint64) error })
	if !ok {
		t.Fatal("decoder has no SeekSample")
	}
	if err := sk.SeekSample(1); err != nil {
		t.Fatal(err)
	}
	next := make([]byte, 6)
	if _, err := d.Read(next); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(next, want[6:]) {
		t.Fatalf("seek dop = % x want % x", next, want[6:])
	}
}

// TestRealDSF is opt-in: set DSD_TEST_FILE to a real .dsf/.dff file.
func TestRealDSF(t *testing.T) {
	path := os.Getenv("DSD_TEST_FILE")
	if path == "" {
		t.Skip("DSD_TEST_FILE not set")
	}
	pr, err := media.ProbeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Codec != "dsd" || pr.Channels != 2 || pr.Rate == 0 || !pr.Decodable {
		t.Fatalf("probe = %+v", pr)
	}
	d, err := openDSD(path, pr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	f := d.Format()
	if f.Format != zat.S24LE || f.Rate != pr.Rate/16 || f.Channels != 2 {
		t.Fatalf("format = %+v probe=%+v", f, pr)
	}
	if d.TotalSamples() == 0 {
		t.Fatal("zero frames")
	}
	buf := make([]byte, 6*4096)
	n, err := d.Read(buf)
	if err != nil && n == 0 {
		t.Fatal(err)
	}
	for i := 0; i+6 <= n; i += 6 {
		want := byte(0x05)
		if (i/6)%2 == 1 {
			want = 0xFA
		}
		if buf[i+2] != want || buf[i+5] != want {
			t.Fatalf("marker at frame %d = %02x/%02x want %02x", i/6, buf[i+2], buf[i+5], want)
		}
	}
	t.Logf("real DSD: %d Hz -> %d Hz DoP, %d frames, read %d bytes", pr.Rate, f.Rate, d.TotalSamples(), n)
}
