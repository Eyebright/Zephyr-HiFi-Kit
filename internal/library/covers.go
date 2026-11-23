package library

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WebP decoding for sidecar art

	"hifihub/server/internal/catalog"
)

// CoverSizes whitelists the thumbnail widths served to the console. Rendering
// happens lazily on first request and is cached, so scans never pay the cost.
var CoverSizes = map[int]bool{96: true, 256: true, 384: true, 640: true}

type coverVariant struct {
	mime   string
	width  int
	height int
	data   []byte
}

// GetCoverVariant returns a cached resized cover, rendering it from the stored
// original on first use. Size must be one of CoverSizes.
func (s *Service) GetCoverVariant(hash string, size int) (string, []byte, error) {
	if !CoverSizes[size] {
		return "", nil, fmt.Errorf("unsupported cover size %d", size)
	}
	mime, data, err := s.loadCoverVariant(hash, size)
	if err == nil {
		return mime, data, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", nil, err
	}
	var src []byte
	if err := s.db.QueryRow(`SELECT data FROM covers WHERE hash=?`, hash).Scan(&src); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, catalog.ErrNotFound
		}
		return "", nil, err
	}
	v, err := renderCoverVariant(src, size)
	if err != nil {
		return "", nil, err
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO cover_variants(hash,size,mime,width,height,data,created_at)
		VALUES(?,?,?,?,?,?,?)`, hash, size, v.mime, v.width, v.height, v.data, time.Now().Unix()); err != nil {
		return "", nil, err
	}
	// Honour a concurrent insert that won the race.
	if mime, data, err = s.loadCoverVariant(hash, size); err != nil {
		return "", nil, err
	}
	return mime, data, nil
}

func (s *Service) loadCoverVariant(hash string, size int) (string, []byte, error) {
	var mime string
	var data []byte
	err := s.db.QueryRow(`SELECT mime,data FROM cover_variants WHERE hash=? AND size=?`, hash, size).Scan(&mime, &data)
	return mime, data, err
}

// renderCoverVariant preserves the aspect ratio, never upscales, and re-encodes
// transparent sources as PNG so logos keep their alpha channel.
func renderCoverVariant(src []byte, size int) (coverVariant, error) {
	img, format, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return coverVariant{}, fmt.Errorf("封面解码失败：%w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return coverVariant{}, errors.New("封面尺寸无效")
	}
	long := w
	if h > long {
		long = h
	}
	scale := float64(size) / float64(long)
	if scale > 1 {
		scale = 1
	}
	nw := int(math.Max(1, math.Round(float64(w)*scale)))
	nh := int(math.Max(1, math.Round(float64(h)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	mime := "image/jpeg"
	if format == "png" || format == "gif" {
		mime = "image/png"
		if err := png.Encode(&buf, dst); err != nil {
			return coverVariant{}, err
		}
	} else if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 86}); err != nil {
		return coverVariant{}, err
	}
	return coverVariant{mime: mime, width: nw, height: nh, data: buf.Bytes()}, nil
}
