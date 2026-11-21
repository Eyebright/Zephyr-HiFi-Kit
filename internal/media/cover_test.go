package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func flacBlock(typ byte, last bool, body []byte) []byte {
	h := []byte{typ, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	if last {
		h[0] |= 0x80
	}
	return append(h, body...)
}

func pictureBody(picType int, mime string, data []byte) []byte {
	b := []byte{}
	put32 := func(v int) { b = binary.BigEndian.AppendUint32(b, uint32(v)) }
	put32(picType)
	put32(len(mime))
	b = append(b, mime...)
	put32(0) // description length
	put32(100)
	put32(100)
	put32(24)
	put32(0)
	put32(len(data))
	return append(b, data...)
}

func writeFLAC(t *testing.T, blocks ...[]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.flac")
	buf := []byte("fLaC")
	for _, b := range blocks {
		buf = append(buf, b...)
	}
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFLACCoverPrefersFront(t *testing.T) {
	other := []byte("other-art")
	front := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	p := writeFLAC(t,
		flacBlock(0, false, make([]byte, 18)),
		flacBlock(1, false, make([]byte, 70*1024)), // padding: must be streamed past
		flacBlock(6, false, pictureBody(0, "image/jpeg", other)),
		flacBlock(6, true, pictureBody(3, "image/png", front)),
	)
	c, err := ReadFLACCover(p)
	if err != nil {
		t.Fatalf("ReadFLACCover: %v", err)
	}
	if c.MIME != "image/png" || string(c.Data) != string(front) {
		t.Fatalf("wrong cover: %+v", c)
	}
}

func TestReadFLACCoverFallbackFirstPicture(t *testing.T) {
	art := []byte("back-cover")
	p := writeFLAC(t,
		flacBlock(0, false, make([]byte, 18)),
		flacBlock(6, true, pictureBody(4, "image/jpeg", art)),
	)
	c, err := ReadFLACCover(p)
	if err != nil || string(c.Data) != string(art) {
		t.Fatalf("fallback cover: %+v %v", c, err)
	}
}

func TestReadFLACCoverNone(t *testing.T) {
	p := writeFLAC(t, flacBlock(0, true, make([]byte, 18)))
	if _, err := ReadFLACCover(p); err != ErrNoCover {
		t.Fatalf("expected ErrNoCover, got %v", err)
	}
}

func TestReadFLACCoverRejectsUnknownMIME(t *testing.T) {
	p := writeFLAC(t,
		flacBlock(0, false, make([]byte, 18)),
		flacBlock(6, true, pictureBody(3, "text/plain", []byte("x"))),
	)
	if _, err := ReadFLACCover(p); err != ErrNoCover {
		t.Fatalf("expected ErrNoCover for text mime, got %v", err)
	}
}

func TestReadFLACCoverNotFLAC(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wav")
	if err := os.WriteFile(p, []byte("RIFFxxxxWAVE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFLACCover(p); err != ErrNoCover {
		t.Fatalf("expected ErrNoCover, got %v", err)
	}
}

func TestReadSidecarCoverPicksConventionalImage(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadSidecarCover(dir)
	if err != nil || c.MIME != "image/png" || len(c.Data) != buf.Len() {
		t.Fatalf("sidecar cover = %+v, %v", c, err)
	}
	if _, err := ReadSidecarCover(t.TempDir()); err != ErrNoCover {
		t.Fatalf("empty dir err = %v", err)
	}
}

func writePNG(t *testing.T, path string, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadSidecarCoverArtworkSubfolder(t *testing.T) {
	dir := t.TempDir()
	// Booklet page is larger, but the conventional COVER name must win.
	writePNG(t, filepath.Join(dir, "Artwork", "01.png"), 8)
	want := writePNG(t, filepath.Join(dir, "Artwork", "COVER.png"), 2)
	c, err := ReadSidecarCover(dir)
	if err != nil || string(c.Data) != string(want) {
		t.Fatalf("artwork cover = %+v, %v", c, err)
	}
}

func TestReadSidecarCoverArtworkSubfolderNumberedOnly(t *testing.T) {
	dir := t.TempDir()
	want := writePNG(t, filepath.Join(dir, "Arwork", "01.png"), 2)
	writePNG(t, filepath.Join(dir, "Arwork", "02.png"), 8)
	c, err := ReadSidecarCover(dir)
	if err != nil || string(c.Data) != string(want) {
		t.Fatalf("numbered artwork cover = %+v, %v", c, err)
	}
}

func TestReadSidecarCoverArbitraryNameFallback(t *testing.T) {
	dir := t.TempDir()
	want := writePNG(t, filepath.Join(dir, "aaa.png"), 2)
	c, err := ReadSidecarCover(dir)
	if err != nil || string(c.Data) != string(want) {
		t.Fatalf("arbitrary cover = %+v, %v", c, err)
	}
}

func TestReadSidecarCoverConventionalBeatsSubfolder(t *testing.T) {
	dir := t.TempDir()
	want := writePNG(t, filepath.Join(dir, "folder.png"), 2)
	writePNG(t, filepath.Join(dir, "Artwork", "COVER.png"), 8)
	c, err := ReadSidecarCover(dir)
	if err != nil || string(c.Data) != string(want) {
		t.Fatalf("dir conventional cover = %+v, %v", c, err)
	}
}
