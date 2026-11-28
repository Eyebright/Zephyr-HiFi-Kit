package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Tags contains descriptive metadata, independent of audio identification.
type Tags struct {
	Title       string
	Artist      string
	Album       string
	Genre       string
	AlbumArtist string
	Composer    string
	Lyrics      string
	Year        int
	DiscNumber  int
	TrackNumber int
}

const (
	maxTagBytes      = 1 << 20
	maxTagValueBytes = 16 << 10
	maxTagBlocks     = 1024
	maxTagEntries    = 4096
)

var errInvalidTags = errors.New("invalid or oversized audio tags")

// ReadTags retains valid fields on malformed input and skips pictures and audio without loading them.
func ReadTags(path string) (Tags, error) { return ReadTagsWithEncoding(path, EncodingAuto) }

// ReadTagsWithEncoding decodes legacy text tags (GBK, Big5, Shift-JIS,
// Windows-1251) when encoding is not auto/UTF-8. Valid UTF-8 is never converted.
func ReadTagsWithEncoding(path, encoding string) (Tags, error) {
	f, err := os.Open(path)
	if err != nil {
		return Tags{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Tags{}, err
	}
	var h [12]byte
	n, err := f.ReadAt(h[:], 0)
	if err != nil && err != io.EOF {
		return Tags{}, err
	}
	if needsAudioMeta(h[:n]) {
		if err := f.Close(); err != nil {
			return Tags{}, err
		}
		return readAudioMetaTags(path)
	}
	return readTags(f, st.Size(), encoding)
}

func readTags(r io.ReaderAt, size int64, encoding string) (Tags, error) {
	var h [12]byte
	n, err := r.ReadAt(h[:], 0)
	if err != nil && err != io.EOF {
		return Tags{}, err
	}
	switch {
	case n >= 4 && string(h[:4]) == "fLaC":
		return readFLACTags(r, size, encoding)
	case n == 12 && string(h[:4]) == "RIFF" && string(h[8:]) == "WAVE":
		end := int64(binary.LittleEndian.Uint32(h[4:8])) + 8
		if end < 12 || end > size {
			return Tags{}, errInvalidTags
		}
		return readWAVTags(r, end, encoding)
	case n >= 3 && string(h[:3]) == "ID3":
		tags, err := parseID3v2(r, 0, size, encoding)
		if err != nil {
			return tags, err
		}
		return preferTags(tags, readID3v1(r, size, encoding)), nil
	case n >= 4 && string(h[:4]) == "DSD ":
		return readDSFTags(r, size, encoding)
	case n >= 11 && string(h[:4]) == "FRM8" && string(h[8:11]) == "DFF":
		return readDFFTags(r, size, encoding)
	case n >= 3 && string(h[:3]) == "TAG":
		return readID3v1(r, size, encoding), nil
	case n == 12 && string(h[:4]) == "FORM" && (string(h[8:]) == "AIFF" || string(h[8:]) == "AIFC"):
		return readAIFFTags(r, size, encoding)
	default:
		return Tags{}, nil
	}
}

func readFLACTags(r io.ReaderAt, size int64, encoding string) (Tags, error) {
	var tags Tags
	off := int64(4)
	for i := 0; i < maxTagBlocks; i++ {
		var h [4]byte
		if off > size-4 {
			return tags, errInvalidTags
		}
		if _, err := r.ReadAt(h[:], off); err != nil {
			return tags, err
		}
		off += 4
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if n > size-off {
			return tags, errInvalidTags
		}
		if h[0]&0x7f == 4 {
			if n > maxTagBytes {
				return tags, errInvalidTags
			}
			buf := make([]byte, int(n))
			if _, err := r.ReadAt(buf, off); err != nil {
				return tags, err
			}
			return parseVorbisTags(buf, encoding)
		}
		off += n
		if h[0]&0x80 != 0 {
			return tags, nil
		}
	}
	return tags, errInvalidTags
}

func parseVorbisTags(b []byte, encoding string) (Tags, error) {
	var tags Tags
	readString := func() ([]byte, bool) {
		if len(b) < 4 {
			return nil, false
		}
		n := binary.LittleEndian.Uint32(b[:4])
		b = b[4:]
		if uint64(n) > uint64(len(b)) {
			return nil, false
		}
		s := b[:int(n)]
		b = b[int(n):]
		return s, true
	}
	if _, ok := readString(); !ok || len(b) < 4 {
		return tags, errInvalidTags
	}
	count := binary.LittleEndian.Uint32(b[:4])
	b = b[4:]
	if count > maxTagEntries {
		return tags, errInvalidTags
	}
	for i := uint32(0); i < count; i++ {
		entry, ok := readString()
		if !ok {
			return tags, errInvalidTags
		}
		rawKey, rawValue, ok := bytes.Cut(entry, []byte("="))
		if !ok {
			continue
		}
		key := strings.ToUpper(string(rawKey))
		if !utf8.ValidString(key) {
			continue
		}
		if isLyricsKey(key) {
			if len(rawValue) <= maxLyricsBytes {
				tags.setLyrics(decodeTagText(rawValue, encoding))
			}
			continue
		}
		if len(entry) > maxTagValueBytes+32 {
			continue
		}
		tags.set(key, decodeTagText(rawValue, encoding))
	}
	return tags, nil
}

func readWAVTags(r io.ReaderAt, end int64, encoding string) (Tags, error) {
	var tags, embedded Tags
	off := int64(12)
	budget := int64(maxTagBytes)
	for i := 0; off < end && i < maxTagBlocks; i++ {
		var h [8]byte
		if off > end-8 {
			return preferTags(embedded, tags), errInvalidTags
		}
		if _, err := r.ReadAt(h[:], off); err != nil {
			return preferTags(embedded, tags), err
		}
		off += 8
		n := int64(binary.LittleEndian.Uint32(h[4:]))
		padded := n + n%2
		if padded > end-off {
			return preferTags(embedded, tags), errInvalidTags
		}
		if string(h[:4]) == "id3 " || string(h[:4]) == "ID3 " {
			if n > maxTagBytes {
				return preferTags(embedded, tags), errInvalidTags
			}
			var err error
			embedded, err = parseID3v2(r, off, n, encoding)
			if err != nil {
				return preferTags(embedded, tags), err
			}
		}
		if string(h[:4]) == "LIST" && n >= 4 {
			var kind [4]byte
			if _, err := r.ReadAt(kind[:], off); err != nil {
				return tags, err
			}
			if string(kind[:]) == "INFO" {
				if n-4 > budget {
					return preferTags(embedded, tags), errInvalidTags
				}
				budget -= n - 4
				buf := make([]byte, int(n-4))
				if len(buf) > 0 {
					if _, err := r.ReadAt(buf, off+4); err != nil {
						return preferTags(embedded, tags), err
					}
				}
				if err := tags.parseWAVInfo(buf, encoding); err != nil {
					return preferTags(embedded, tags), err
				}
			}
		}
		off += padded
	}
	if off != end {
		return preferTags(embedded, tags), errInvalidTags
	}
	return preferTags(embedded, tags), nil
}

func (tags *Tags) parseWAVInfo(b []byte, encoding string) error {
	for i := 0; len(b) > 0 && i < maxTagEntries; i++ {
		if len(b) < 8 {
			return errInvalidTags
		}
		key := string(b[:4])
		n := uint64(binary.LittleEndian.Uint32(b[4:8]))
		b = b[8:]
		padded := n + n%2
		if padded > uint64(len(b)) {
			return errInvalidTags
		}
		if n <= maxTagValueBytes {
			switch key {
			case "INAM":
				tags.set("TITLE", decodeTagText(b[:int(n)], encoding))
			case "IART":
				tags.set("ARTIST", decodeTagText(b[:int(n)], encoding))
			case "IPRD":
				tags.set("ALBUM", decodeTagText(b[:int(n)], encoding))
			case "IGNR":
				tags.set("GENRE", decodeTagText(b[:int(n)], encoding))
			case "ICRD":
				tags.set("DATE", decodeTagText(b[:int(n)], encoding))
			case "ITRK", "IPRT":
				tags.set("TRACKNUMBER", decodeTagText(b[:int(n)], encoding))
			case "IMUS": // RIFF INFO music composer (IWRI is the writer).
				tags.set("COMPOSER", decodeTagText(b[:int(n)], encoding))
			}
		}
		b = b[int(padded):]
	}
	if len(b) != 0 {
		return errInvalidTags
	}
	return nil
}

func (tags *Tags) set(key, value string) {
	if len(value) > maxTagValueBytes || !utf8.ValidString(value) {
		return
	}
	value = strings.TrimSpace(strings.Trim(value, "\x00"))
	var dst *string
	var number *int
	switch key {
	case "TITLE":
		dst = &tags.Title
	case "ARTIST":
		dst = &tags.Artist
	case "ALBUM":
		dst = &tags.Album
	case "ALBUMARTIST", "ALBUM_ARTIST", "ALBUM ARTIST":
		dst = &tags.AlbumArtist
	case "GENRE":
		dst = &tags.Genre
	case "COMPOSER":
		dst = &tags.Composer
	case "DATE", "YEAR":
		number = &tags.Year
	case "DISCNUMBER", "DISC":
		number = &tags.DiscNumber
	case "TRACKNUMBER", "TRACK":
		number = &tags.TrackNumber
	default:
		return
	}
	if number != nil {
		if *number == 0 {
			v := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '-' || r == '\x00' })
			if len(v) > 0 && !strings.HasPrefix(value, "-") {
				n, err := strconv.Atoi(strings.TrimSpace(v[0]))
				if err == nil && n > 0 && n <= 9999 {
					*number = n
				}
			}
		}
		return
	}
	multiple := key == "GENRE" || key == "COMPOSER"
	for _, v := range strings.FieldsFunc(value, func(r rune) bool { return r == '\x00' || (multiple && r == ';') }) {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !multiple {
			if *dst == "" {
				*dst = v
			}
			return
		}
		duplicate := false
		for _, old := range strings.Split(*dst, "; ") {
			if strings.EqualFold(old, v) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			joined := v
			if *dst != "" {
				joined = *dst + "; " + v
			}
			if len(joined) <= maxTagValueBytes {
				*dst = joined
			}
		}
	}
}
