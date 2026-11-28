package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"strings"
)

// ReadEmbeddedCover extracts album art from the container's native tag area:
// FLAC PICTURE, ID3v2 APIC/PIC (MP3, DSF, DFF, WAV/AIFF ID3 chunks), MP4 covr,
// Ogg/Opus comment pictures and APEv2 cover items. Containers without a
// picture convention report ErrNoCover so callers fall back to sidecar images.
func ReadEmbeddedCover(path, container string) (Cover, error) {
	switch container {
	case "flac":
		return ReadFLACCover(path)
	case "mp3", "dsf", "dff", "wav", "aiff", "adts":
		return ReadID3Cover(path)
	case "m4a", "mp4":
		return ReadMP4Cover(path)
	case "ogg", "opus":
		return ReadOggCover(path)
	case "ape", "mpc", "wv", "tak":
		return ReadAPECover(path)
	}
	return Cover{}, ErrNoCover
}

// ReadID3Cover locates the ID3v2 tag in any container that carries one and
// returns its front cover (picture type 3), falling back to the first picture.
func ReadID3Cover(path string) (Cover, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cover{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Cover{}, err
	}
	offset, available, err := id3Location(f, info.Size())
	if err != nil {
		return Cover{}, err
	}
	var front, fallback *Cover
	err = walkID3v2(f, offset, available, func(version byte, id string, data []byte) {
		if (version == 2 && id == "PIC") || (version != 2 && id == "APIC") {
			if p, ok := parseAPIC(version, data); ok {
				if p.typ == 3 && front == nil {
					front = &p.cover
				} else if fallback == nil {
					fallback = &p.cover
				}
			}
		}
	})
	if front != nil {
		return *front, nil
	}
	if fallback != nil {
		return *fallback, nil
	}
	if err != nil && err != errInvalidTags {
		return Cover{}, err
	}
	return Cover{}, ErrNoCover
}

// id3Location finds the ID3v2 tag offset and bounded length per container.
func id3Location(r io.ReaderAt, size int64) (int64, int64, error) {
	if size < 12 {
		return 0, 0, ErrNoCover
	}
	var h [12]byte
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return 0, 0, err
	}
	if string(h[:3]) == "ID3" {
		return 0, size, nil
	}
	switch string(h[:4]) {
	case "DSD ":
		if size < 28 {
			return 0, 0, ErrNoCover
		}
		var p [16]byte
		if _, err := r.ReadAt(p[:], 20); err != nil {
			return 0, 0, err
		}
		offset := int64(binary.LittleEndian.Uint64(p[:8]))
		n := int64(binary.LittleEndian.Uint64(p[8:]))
		if offset < 28 || offset > size {
			return 0, 0, ErrNoCover
		}
		if n > 0 && n <= maxTagBytes && n <= size-offset {
			return offset, n, nil
		}
		return offset, size - offset, nil
	case "RIFF", "FORM":
		return chunkID3(r, size, 12, false)
	case "FRM8":
		return chunkID3(r, size, 11, true)
	}
	return 0, 0, ErrNoCover
}

func chunkID3(r io.ReaderAt, size, off int64, be bool) (int64, int64, error) {
	for i := 0; off+8 <= size && i < maxTagBlocks; i++ {
		var ch [8]byte
		if _, err := r.ReadAt(ch[:], off); err != nil {
			return 0, 0, err
		}
		var n int64
		if be {
			n = int64(binary.BigEndian.Uint32(ch[4:8]))
		} else {
			n = int64(binary.LittleEndian.Uint32(ch[4:8]))
		}
		body := off + 8
		if string(ch[:4]) == "ID3 " || string(ch[:4]) == "id3 " {
			if n > maxTagBytes || body+n > size {
				return 0, 0, ErrNoCover
			}
			return body, n, nil
		}
		off = body + n + n%2
	}
	return 0, 0, ErrNoCover
}

// parseAPIC decodes an APIC (v2.3/v2.4) or PIC (v2.2) frame body.
func parseAPIC(version byte, data []byte) (picture, bool) {
	if len(data) < 4 {
		return picture{}, false
	}
	enc := data[0]
	p := 1
	mime := ""
	if version == 2 {
		if p+3 > len(data) {
			return picture{}, false
		}
		switch string(data[p : p+3]) {
		case "JPG":
			mime = "image/jpeg"
		case "PNG":
			mime = "image/png"
		}
		p += 3
	} else {
		i := bytes.IndexByte(data[p:], 0)
		if i < 0 {
			return picture{}, false
		}
		mime = string(data[p : p+i])
		p += i + 1
	}
	if p >= len(data) {
		return picture{}, false
	}
	typ := int(data[p])
	p++
	if enc == 1 || enc == 2 {
		found := false
		for p+1 < len(data) {
			if data[p] == 0 && data[p+1] == 0 {
				p += 2
				found = true
				break
			}
			p += 2
		}
		if !found {
			return picture{}, false
		}
	} else {
		i := bytes.IndexByte(data[p:], 0)
		if i < 0 {
			return picture{}, false
		}
		p += i + 1
	}
	img := data[p:]
	if mime == "-->" || len(img) == 0 {
		return picture{}, false
	}
	if mime == "" {
		mime = http.DetectContentType(img)
	}
	if !ValidCoverData(mime, img) {
		return picture{}, false
	}
	return picture{typ: typ, cover: Cover{MIME: mime, Data: img}}, true
}

// ReadMP4Cover walks moov.udta.meta.ilst.covr (or moov.udta.ilst.covr) and
// returns the first valid picture data atom.
func ReadMP4Cover(path string) (Cover, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cover{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Cover{}, err
	}
	var found *Cover
	var walk func(off, end int64, depth int) error
	walk = func(off, end int64, depth int) error {
		if depth > 8 {
			return nil
		}
		for off+8 <= end {
			var h [8]byte
			if _, err := f.ReadAt(h[:], off); err != nil {
				return err
			}
			n := int64(binary.BigEndian.Uint32(h[:4]))
			typ := string(h[4:8])
			hdr := int64(8)
			if n == 1 {
				if off+16 > end {
					break
				}
				var e [8]byte
				if _, err := f.ReadAt(e[:], off+8); err != nil {
					return err
				}
				n = int64(binary.BigEndian.Uint64(e[:]))
				hdr = 16
			}
			if n < hdr || off+n > end {
				break
			}
			body := off + hdr
			switch typ {
			case "moov", "udta", "ilst":
				if err := walk(body, off+n, depth+1); err != nil {
					return err
				}
			case "meta":
				if err := walk(body+4, off+n, depth+1); err != nil {
					return err
				}
			case "covr":
				for doff := body; doff+8 <= off+n; {
					var dh [8]byte
					if _, err := f.ReadAt(dh[:], doff); err != nil {
						return err
					}
					dn := int64(binary.BigEndian.Uint32(dh[:4]))
					if dn < 8 || doff+dn > off+n {
						break
					}
					if string(dh[4:8]) == "data" && dn > 16 {
						buf := make([]byte, dn-16)
						if _, err := f.ReadAt(buf, doff+16); err != nil {
							return err
						}
						mime := http.DetectContentType(buf)
						if ValidCoverData(mime, buf) && found == nil {
							found = &Cover{MIME: mime, Data: buf}
						}
					}
					doff += dn
				}
			}
			off += n
		}
		return nil
	}
	if err := walk(0, info.Size(), 0); err != nil {
		return Cover{}, err
	}
	if found != nil {
		return *found, nil
	}
	return Cover{}, ErrNoCover
}

// ReadOggCover reassembles the first Ogg packets and reads the comment header
// (OpusTags or Vorbis comment) for METADATA_BLOCK_PICTURE / COVERART entries.
func ReadOggCover(path string) (Cover, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cover{}, err
	}
	defer f.Close()
	var pkt []byte
	var total int64
	for i := 0; i < 64; i++ {
		var page [27]byte
		if _, err := io.ReadFull(f, page[:]); err != nil {
			break
		}
		if string(page[:4]) != "OggS" {
			break
		}
		seg := make([]byte, int(page[26]))
		if _, err := io.ReadFull(f, seg); err != nil {
			break
		}
		ps := 0
		for _, v := range seg {
			ps += int(v)
		}
		total += int64(ps)
		if total > maxTagBytes {
			break
		}
		payload := make([]byte, ps)
		if _, err := io.ReadFull(f, payload); err != nil {
			break
		}
		pos := 0
		for _, v := range seg {
			pkt = append(pkt, payload[pos:pos+int(v)]...)
			pos += int(v)
			if v < 255 {
				if c, ok := oggCommentCover(pkt); ok {
					return c, nil
				}
				pkt = pkt[:0]
			}
		}
	}
	return Cover{}, ErrNoCover
}

func oggCommentCover(pkt []byte) (Cover, bool) {
	var body []byte
	switch {
	case bytes.HasPrefix(pkt, []byte("OpusTags")):
		body = pkt[8:]
	case len(pkt) > 7 && pkt[0] == 3 && string(pkt[1:7]) == "vorbis":
		body = pkt[7:]
	default:
		return Cover{}, false
	}
	if len(body) < 8 {
		return Cover{}, false
	}
	vl := int(binary.LittleEndian.Uint32(body[:4]))
	if vl > len(body)-4 {
		return Cover{}, false
	}
	p := 4 + vl
	count := int(binary.LittleEndian.Uint32(body[p : p+4]))
	p += 4
	for i := 0; i < count && i < maxTagEntries; i++ {
		if p+4 > len(body) {
			break
		}
		n := int(binary.LittleEndian.Uint32(body[p : p+4]))
		p += 4
		if n > len(body)-p {
			break
		}
		entry := string(body[p : p+n])
		p += n
		switch {
		case strings.HasPrefix(entry, "METADATA_BLOCK_PICTURE="):
			raw, err := base64.StdEncoding.DecodeString(entry[len("METADATA_BLOCK_PICTURE="):])
			if err != nil || len(raw) > maxCoverBytes+1024 {
				continue
			}
			if pic, ok := parsePicture(raw); ok {
				return pic.cover, true
			}
		case strings.HasPrefix(entry, "COVERART="):
			raw, err := base64.StdEncoding.DecodeString(entry[len("COVERART="):])
			if err != nil {
				continue
			}
			mime := http.DetectContentType(raw)
			if ValidCoverData(mime, raw) {
				return Cover{MIME: mime, Data: raw}, true
			}
		}
	}
	return Cover{}, false
}

// ReadAPECover reads APEv2 items (footer at EOF, or a leading header) and
// returns "Cover Art (Front)" first, then any other cover item.
func ReadAPECover(path string) (Cover, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cover{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Cover{}, err
	}
	size := info.Size()
	if size < 32 {
		return Cover{}, ErrNoCover
	}
	var start, end, items int64
	var foot [32]byte
	if _, err := f.ReadAt(foot[:], size-32); err != nil {
		return Cover{}, err
	}
	if string(foot[:8]) == "APETAGEX" {
		n := int64(binary.LittleEndian.Uint32(foot[12:16]))
		if n < 32 || n > size || n > maxTagBytes+32 {
			return Cover{}, ErrNoCover
		}
		items = int64(binary.LittleEndian.Uint32(foot[16:20]))
		start, end = size-n, size-32
		if binary.LittleEndian.Uint32(foot[20:24])&0x80000000 != 0 {
			var hh [8]byte
			if _, err := f.ReadAt(hh[:], start); err != nil {
				return Cover{}, err
			}
			if string(hh[:8]) == "APETAGEX" {
				start += 32
			}
		}
	} else {
		var hh [32]byte
		if _, err := f.ReadAt(hh[:], 0); err != nil {
			return Cover{}, err
		}
		if string(hh[:8]) != "APETAGEX" {
			return Cover{}, ErrNoCover
		}
		n := int64(binary.LittleEndian.Uint32(hh[12:16]))
		if n < 32 || 32+n > size || n > maxTagBytes+32 {
			return Cover{}, ErrNoCover
		}
		items = int64(binary.LittleEndian.Uint32(hh[16:20]))
		start, end = 32, 32+n-32
	}
	if end <= start || end-start > maxTagBytes {
		return Cover{}, ErrNoCover
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil {
		return Cover{}, err
	}
	var fallback *Cover
	p := 0
	for i := int64(0); i < items && i < maxTagEntries && p+8 <= len(buf); i++ {
		vs := int(binary.LittleEndian.Uint32(buf[p : p+4]))
		p += 8
		ki := bytes.IndexByte(buf[p:], 0)
		if ki < 0 {
			break
		}
		key := string(buf[p : p+ki])
		p += ki + 1
		if vs > len(buf)-p {
			break
		}
		val := buf[p : p+vs]
		p += vs
		if !strings.HasPrefix(key, "Cover Art") {
			continue
		}
		img := val
		if ni := bytes.IndexByte(val, 0); ni >= 0 {
			img = val[ni+1:]
		}
		mime := http.DetectContentType(img)
		if !ValidCoverData(mime, img) {
			continue
		}
		c := Cover{MIME: mime, Data: img}
		if strings.Contains(key, "Front") {
			return c, nil
		}
		if fallback == nil {
			fallback = &c
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Cover{}, ErrNoCover
}