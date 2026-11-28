package library

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"testing"
)

func testCoverImage(t *testing.T, w, h int, format string) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRenderCoverVariantKeepsAspectAndSkipsUpscale(t *testing.T) {
	v, err := renderCoverVariant(testCoverImage(t, 800, 600, "png"), 256)
	if err != nil {
		t.Fatal(err)
	}
	if v.width != 256 || v.height != 192 || v.mime != "image/png" {
		t.Fatalf("png variant: %+v", v)
	}
	if _, format, err := image.Decode(bytes.NewReader(v.data)); err != nil || format != "png" {
		t.Fatalf("decode png variant: %q %v", format, err)
	}
	v, err = renderCoverVariant(testCoverImage(t, 1000, 500, "jpg"), 640)
	if err != nil || v.width != 640 || v.height != 320 || v.mime != "image/jpeg" {
		t.Fatalf("jpeg variant: %+v %v", v, err)
	}
	small := testCoverImage(t, 50, 40, "jpg")
	v, err = renderCoverVariant(small, 640)
	if err != nil || v.width != 50 || v.height != 40 {
		t.Fatalf("variant must not upscale: %+v %v", v, err)
	}
}

func TestGetCoverVariantIsCached(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	src := testCoverImage(t, 640, 640, "png")
	if _, err := st.DB.Exec(`INSERT INTO covers(hash,mime,data,created_at) VALUES('h1','image/png',?,1)`, src); err != nil {
		t.Fatal(err)
	}
	mime, data, err := s.GetCoverVariant("h1", 96)
	if err != nil || mime == "" || len(data) == 0 {
		t.Fatalf("first variant: %q %d %v", mime, len(data), err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM cover_variants WHERE hash='h1' AND size=96`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("variant row: %d %v", n, err)
	}
	again, data2, err := s.GetCoverVariant("h1", 96)
	if err != nil || again != mime || string(data2) != string(data) {
		t.Fatalf("cached variant mismatch: %v", err)
	}
	if _, _, err := s.GetCoverVariant("h1", 123); err == nil {
		t.Fatal("unsupported size accepted")
	}
	if _, _, err := s.GetCoverVariant("missing", 96); err == nil {
		t.Fatal("missing cover accepted")
	}
	// Pruning covers must drop their rendered variants too.
	s.pruneCovers()
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM cover_variants`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("variants not pruned: %d %v", n, err)
	}
}

func TestSidecarParent(t *testing.T) {
	root := filepath.FromSlash("//nas/music")
	cases := []struct{ dir, want string }{
		{filepath.Join(root, "Album"), ""},
		{filepath.Join(root, "Album", "Disc 1"), filepath.Join(root, "Album")},
		{filepath.Join(root, "Album", "Disc 1", "CD2"), filepath.Join(root, "Album", "Disc 1")},
		{root + string(filepath.Separator), ""},
		{filepath.FromSlash("//nas/other/Album"), ""},
		{filepath.FromSlash("D:/music/Album"), ""},
	}
	for _, c := range cases {
		if got := sidecarParent(c.dir, root); got != c.want {
			t.Errorf("sidecarParent(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}
