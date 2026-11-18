package media

import (
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
func ReadTags(path string) (Tags, error) {
	f, err := os.Open(path)
	if err != nil {
		return Tags{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Tags{}, err
	}
	return readTags(f, st.Size())
}

func readTags(r io.ReaderAt, size int64) (Tags, error) {
	var h [12]byte
	n, err := r.ReadAt(h[:], 0)
	if err != nil && err != io.EOF {
		return Tags{}, err
	}
	switch {
	case n >= 4 && string(h[:4]) == "fLaC":
		return readFLACTags(r, size)
	case n == 12 && string(h[:4]) == "RIFF" && string(h[8:]) == "WAVE":
		end := int64(binary.LittleEndian.Uint32(h[4:8])) + 8
		if end < 12 || end > size {
			return Tags{}, errInvalidTags
		}
		return readWAVTags(r, end)
	default:
		return Tags{}, nil
	}
}

func readFLACTags(r io.ReaderAt, size int64) (Tags, error) {
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
			return parseVorbisTags(buf)
		}
		off += n
		if h[0]&0x80 != 0 {
			return tags, nil
		}
	}
	return tags, errInvalidTags
}

func parseVorbisTags(b []byte) (Tags, error) {
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
		if len(entry) > maxTagValueBytes+32 {
			continue
		}
		key, value, ok := strings.Cut(string(entry), "=")
		if ok {
			tags.set(strings.ToUpper(key), value)
		}
	}
	return tags, nil
}

func readWAVTags(r io.ReaderAt, end int64) (Tags, error) {
	var tags Tags
	off := int64(12)
	budget := int64(maxTagBytes)
	for i := 0; off < end && i < maxTagBlocks; i++ {
		var h [8]byte
		if off > end-8 {
			return tags, errInvalidTags
		}
		if _, err := r.ReadAt(h[:], off); err != nil {
			return tags, err
		}
		off += 8
		n := int64(binary.LittleEndian.Uint32(h[4:]))
		padded := n + n%2
		if padded > end-off {
			return tags, errInvalidTags
		}
		if string(h[:4]) == "LIST" && n >= 4 {
			var kind [4]byte
			if _, err := r.ReadAt(kind[:], off); err != nil {
				return tags, err
			}
			if string(kind[:]) == "INFO" {
				if n-4 > budget {
					return tags, errInvalidTags
				}
				budget -= n - 4
				buf := make([]byte, int(n-4))
				if len(buf) > 0 {
					if _, err := r.ReadAt(buf, off+4); err != nil {
						return tags, err
					}
				}
				if err := tags.parseWAVInfo(buf); err != nil {
					return tags, err
				}
			}
		}
		off += padded
	}
	if off != end {
		return tags, errInvalidTags
	}
	return tags, nil
}

func (tags *Tags) parseWAVInfo(b []byte) error {
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
				tags.set("TITLE", string(b[:int(n)]))
			case "IART":
				tags.set("ARTIST", string(b[:int(n)]))
			case "IPRD":
				tags.set("ALBUM", string(b[:int(n)]))
			case "IGNR":
				tags.set("GENRE", string(b[:int(n)]))
			case "ICRD":
				tags.set("DATE", string(b[:int(n)]))
			case "ITRK", "IPRT":
				tags.set("TRACKNUMBER", string(b[:int(n)]))
			case "IMUS": // RIFF INFO music composer (IWRI is the writer).
				tags.set("COMPOSER", string(b[:int(n)]))
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
