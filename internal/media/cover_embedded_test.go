package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	return testPNGSize(t, 2)
}

func testPNGSize(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, n, n))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

func apicFrame(mime string, typ byte, img []byte) []byte {
	payload := append([]byte{0}, mime...)
	payload = append(payload, 0, typ)
	payload = append(payload, "desc"...)
	payload = append(payload, 0)
	payload = append(payload, img...)
	f := append([]byte("APIC"), byte(len(payload)>>24), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
	f = append(f, 0, 0)
	return append(f, payload...)
}

func id3Blob(frames ...[]byte) []byte {
	var body []byte
	for _, f := range frames {
		body = append(body, f...)
	}
	out := append([]byte("ID3"), 3, 0, 0)
	out = append(out, synchsafe(len(body))...)
	return append(out, body...)
}

func pictureBlock(typ int, mime string, img []byte) []byte {
	be32 := func(n int) []byte {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(n))
		return b[:]
	}
	out := be32(typ)
	out = append(out, be32(len(mime))...)
	out = append(out, mime...)
	out = append(out, be32(0)...)
	out = append(out, make([]byte, 16)...)
	out = append(out, be32(len(img))...)
	return append(out, img...)
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func le64(n int64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(n))
	return b[:]
}

func TestReadID3CoverContainers(t *testing.T) {
	img := testPNG(t)
	blob := id3Blob(apicFrame("image/png", 3, img))
	wav := append([]byte("RIFF"), 0, 0, 0, 0)
	wav = append(wav, "WAVE"...)
	wav = append(wav, "ID3 "...)
	wav = append(wav, byte(len(blob)), byte(len(blob)>>8), byte(len(blob)>>16), byte(len(blob)>>24))
	wav = append(wav, blob...)
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	dsf := append([]byte("DSD "), le64(28)...)
	dsf = append(dsf, le64(int64(28+len(blob)))...)
	dsf = append(dsf, le64(28)...)
	dsf = append(dsf, blob...)
	cases := []struct {
		name      string
		container string
		data      []byte
	}{
		{"t.mp3", "mp3", blob},
		{"t.dsf", "dsf", dsf},
		{"t.wav", "wav", wav},
	}
	for _, c := range cases {
		p := writeTemp(t, c.name, c.data)
		got, err := ReadEmbeddedCover(p, c.container)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.Equal(got.Data, img) || got.MIME != "image/png" {
			t.Fatalf("%s: cover mismatch", c.name)
		}
	}
}

func TestReadID3CoverPrefersFront(t *testing.T) {
	other, front := testPNGSize(t, 3), testPNG(t)
	blob := id3Blob(apicFrame("image/png", 0, other), apicFrame("image/png", 3, front))
	p := writeTemp(t, "t.mp3", blob)
	got, err := ReadID3Cover(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, front) {
		t.Fatal("expected front cover (type 3)")
	}
}

func TestReadID3CoverV22PIC(t *testing.T) {
	img := testPNG(t)
	payload := append([]byte{0}, "PNG"...)
	payload = append(payload, 3)
	payload = append(payload, "d"...)
	payload = append(payload, 0)
	payload = append(payload, img...)
	frame := append([]byte("PIC"), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
	frame = append(frame, payload...)
	body := frame
	blob := append([]byte("ID3"), 2, 0, 0)
	blob = append(blob, synchsafe(len(body))...)
	blob = append(blob, body...)
	p := writeTemp(t, "t.mp3", blob)
	got, err := ReadID3Cover(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, img) {
		t.Fatal("v2.2 PIC cover mismatch")
	}
}

func coverMP4Atom(typ string, payload []byte) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(8+len(payload)))
	out = append(out, typ...)
	return append(out, payload...)
}

func TestReadMP4Cover(t *testing.T) {
	img := testPNG(t)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 13)
	data = append(data, 0, 0, 0, 0)
	data = append(data, img...)
	covr := coverMP4Atom("covr", coverMP4Atom("data", data))
	ilst := coverMP4Atom("ilst", covr)
	meta := coverMP4Atom("meta", append([]byte{0, 0, 0, 0}, ilst...))
	moov := coverMP4Atom("moov", coverMP4Atom("udta", meta))
	file := append(coverMP4Atom("ftyp", []byte("M4A ")), moov...)
	p := writeTemp(t, "t.m4a", file)
	got, err := ReadEmbeddedCover(p, "m4a")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, img) {
		t.Fatal("m4a covr mismatch")
	}
}

func coverOggPage(packet []byte) []byte {
	var seg []byte
	n := len(packet)
	for n >= 255 {
		seg = append(seg, 255)
		n -= 255
	}
	seg = append(seg, byte(n))
	page := append([]byte("OggS"), 0, 0)
	page = append(page, make([]byte, 8)...)
	page = append(page, 1, 0, 0, 0)
	page = append(page, 0, 0, 0, 0)
	page = append(page, 0, 0, 0, 0)
	page = append(page, byte(len(seg)))
	page = append(page, seg...)
	return append(page, packet...)
}

func TestReadOggCover(t *testing.T) {
	img := testPNG(t)
	entry := "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(pictureBlock(3, "image/png", img))
	pkt := append([]byte("OpusTags"), 0, 0, 0, 0)
	pkt = append(pkt, 1, 0, 0, 0)
	pkt = append(pkt, byte(len(entry)), byte(len(entry)>>8), byte(len(entry)>>16), byte(len(entry)>>24))
	pkt = append(pkt, entry...)
	p := writeTemp(t, "t.opus", coverOggPage(pkt))
	got, err := ReadEmbeddedCover(p, "ogg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, img) {
		t.Fatal("ogg picture mismatch")
	}
}

func TestReadAPECover(t *testing.T) {
	img := testPNG(t)
	val := append([]byte("cover.png"), 0)
	val = append(val, img...)
	key := append([]byte("Cover Art (Front)"), 0)
	item := make([]byte, 4)
	binary.LittleEndian.PutUint32(item, uint32(len(val)))
	item = append(item, 0, 0, 0, 0)
	item = append(item, key...)
	item = append(item, val...)
	footer := make([]byte, 32)
	copy(footer, "APETAGEX")
	binary.LittleEndian.PutUint32(footer[8:12], 2000)
	binary.LittleEndian.PutUint32(footer[12:16], uint32(len(item)+32))
	binary.LittleEndian.PutUint32(footer[16:20], 1)
	p := writeTemp(t, "t.ape", append(item, footer...))
	got, err := ReadEmbeddedCover(p, "ape")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, img) {
		t.Fatal("ape cover mismatch")
	}
}

func TestReadEmbeddedCoverNoArt(t *testing.T) {
	wav := append([]byte("RIFF"), 0, 0, 0, 0)
	wav = append(wav, "WAVEfmt "...)
	wav = append(wav, 16, 0, 0, 0)
	wav = append(wav, make([]byte, 16)...)
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	for _, c := range []struct{ name, container string }{{"t.wav", "wav"}, {"t.mp3", "mp3"}, {"t.m4a", "m4a"}} {
		p := writeTemp(t, c.name, wav)
		if _, err := ReadEmbeddedCover(p, c.container); err != ErrNoCover {
			t.Fatalf("%s: want ErrNoCover, got %v", c.name, err)
		}
	}
}