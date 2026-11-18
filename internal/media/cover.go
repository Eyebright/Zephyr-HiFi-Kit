package media

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// Cover is embedded album art extracted from an audio container.
type Cover struct {
	MIME string
	Data []byte
}

// ErrNoCover reports that the file carries no usable embedded picture.
var ErrNoCover = errors.New("no embedded cover")

// maxCoverBytes bounds embedded art; larger pictures are skipped, not truncated.
const maxCoverBytes = 8 << 20

var coverMIMEs = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// ReadFLACCover walks the FLAC metadata blocks and returns the front cover
// (picture type 3), falling back to the first picture block. Blocks are streamed
// so a large VORBIS_COMMENT or padding block never lands in memory.
func ReadFLACCover(path string) (Cover, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cover{}, err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return Cover{}, ErrNoCover
	}
	var fallback *Cover
	for i := 0; i < 64; i++ {
		var h [4]byte
		if _, err := io.ReadFull(f, h[:]); err != nil {
			break
		}
		last := h[0]&0x80 != 0
		typ := h[0] & 0x7f
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if typ == 6 && n <= maxCoverBytes+1024 {
			buf := make([]byte, n)
			if _, err := io.ReadFull(f, buf); err != nil {
				break
			}
			if c, ok := parsePicture(buf); ok {
				if c.typ == 3 {
					return c.cover, nil
				}
				if fallback == nil {
					fallback = &c.cover
				}
			}
		} else if _, err := io.CopyN(io.Discard, f, n); err != nil {
			break
		}
		if last {
			break
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Cover{}, ErrNoCover
}

type picture struct {
	typ   int
	cover Cover
}

// parsePicture decodes a FLAC PICTURE block body (RFC 9639 §8.8 layout).
func parsePicture(b []byte) (picture, bool) {
	be32 := func(off int) (int, bool) {
		if off+4 > len(b) {
			return 0, false
		}
		return int(binary.BigEndian.Uint32(b[off : off+4])), true
	}
	typ, ok := be32(0)
	if !ok {
		return picture{}, false
	}
	p := 4
	mimeLen, ok := be32(p)
	if !ok || mimeLen > 64 || p+4+mimeLen > len(b) {
		return picture{}, false
	}
	mime := string(b[p+4 : p+4+mimeLen])
	p += 4 + mimeLen
	descLen, ok := be32(p)
	if !ok || p+4+descLen > len(b) {
		return picture{}, false
	}
	p += 4 + descLen
	p += 16 // width, height, depth, colors
	dataLen, ok := be32(p)
	if !ok || dataLen <= 0 || dataLen > maxCoverBytes || p+4+dataLen > len(b) {
		return picture{}, false
	}
	if !coverMIMEs[mime] {
		return picture{}, false
	}
	return picture{typ: typ, cover: Cover{MIME: mime, Data: b[p+4 : p+4+dataLen]}}, true
}