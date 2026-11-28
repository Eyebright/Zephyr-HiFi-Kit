package media

import (
	"bytes"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func synchsafeSize(b []byte) (int64, bool) {
	var n int64
	for _, c := range b {
		if c&0x80 != 0 {
			return 0, false
		}
		n = n<<7 | int64(c)
	}
	return n, true
}

func removeUnsynchronisation(b []byte) []byte {
	if !bytes.Contains(b, []byte{0xff, 0x00}) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == 0xff && i+1 < len(b) && b[i+1] == 0 {
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func decodeUTF16(b []byte) (string, bool) {
	if len(b) == 0 {
		return "", true
	}
	var order binary.ByteOrder = binary.BigEndian
	if len(b) >= 2 {
		switch {
		case b[0] == 0xff && b[1] == 0xfe:
			order = binary.LittleEndian
			b = b[2:]
		case b[0] == 0xfe && b[1] == 0xff:
			b = b[2:]
		}
	}
	if len(b)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = order.Uint16(b[i*2:])
	}
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		r := rune(units[i])
		if units[i] >= 0xd800 && units[i] <= 0xdbff {
			if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return "", false
			}
			i++
			r = utf16.DecodeRune(rune(units[i-1]), rune(units[i]))
		} else if units[i] >= 0xdc00 && units[i] <= 0xdfff {
			return "", false
		}
		runes = append(runes, r)
	}
	return string(runes), true
}

func decodeLegacyTagText(b []byte, encoding string) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	enc, ok := NormalizeTextEncoding(encoding)
	if !ok {
		return ""
	}
	if enc != EncodingAuto && enc != EncodingUTF8 {
		if s, ok := decodeWithEncoding(b, enc); ok {
			return s
		}
		return ""
	}
	if enc == EncodingUTF8 {
		return ""
	}
	latin := make([]rune, len(b))
	for i, c := range b {
		latin[i] = rune(c)
	}
	latinText := string(latin)
	if legacy := autoDecode(b); textScore(legacy) > textScore(latinText) {
		return legacy
	}
	return latinText
}

func id3TextValues(b []byte, version byte, encoding string) []string {
	if len(b) == 0 {
		return nil
	}
	coding := b[0]
	b = b[1:]
	var values []string
	if coding == 1 || coding == 2 {
		s, ok := decodeUTF16(b)
		if !ok {
			return nil
		}
		values = strings.Split(s, "\x00")
	} else {
		for _, raw := range bytes.Split(b, []byte{0}) {
			var s string
			switch coding {
			case 0:
				s = decodeLegacyTagText(raw, encoding)
			case 3:
				s = decodeTagText(raw, encoding)
			default:
				continue
			}
			values = append(values, s)
		}
	}
	return values
}

func id3Field(version byte, id string) string {
	if version == 2 {
		switch id {
		case "TT2":
			return "TITLE"
		case "TP1":
			return "ARTIST"
		case "TAL":
			return "ALBUM"
		case "TP2":
			return "ALBUMARTIST"
		case "TCM":
			return "COMPOSER"
		case "TYE":
			return "YEAR"
		case "TRK":
			return "TRACKNUMBER"
		case "TPA":
			return "DISCNUMBER"
		case "TCO":
			return "GENRE"
		case "ULT", "SLT":
			return "LYRICS"
		}
		return ""
	}
	switch id {
	case "TIT2":
		return "TITLE"
	case "TPE1":
		return "ARTIST"
	case "TALB":
		return "ALBUM"
	case "TPE2":
		return "ALBUMARTIST"
	case "TCOM":
		return "COMPOSER"
	case "TDRC", "TYER":
		return "YEAR"
	case "TRCK":
		return "TRACKNUMBER"
	case "TPOS":
		return "DISCNUMBER"
	case "TCON":
		return "GENRE"
	case "USLT", "SYLT":
		return "LYRICS"
	}
	return ""
}

func parseID3v2(r io.ReaderAt, offset, available int64, encoding string) (Tags, error) {
	var tags Tags
	err := walkID3v2(r, offset, available, func(version byte, id string, data []byte) {
		field := id3Field(version, id)
		if field == "LYRICS" {
			if s, ok := parseID3LyricsFrame(id, data, encoding); ok {
				tags.setLyrics(s)
			}
			return
		}
		if field != "" && len(data) <= maxTagValueBytes+1 {
			for _, value := range id3TextValues(data, version, encoding) {
				tags.set(field, value)
			}
		}
	})
	return tags, err
}

// walkID3v2 iterates the frames of an ID3v2 tag, handing each decoded frame
// payload to visit. Text and picture readers share it so frame quirks (stray
// DSF byte, unsynchronisation, v2.2 sizes) are handled in one place.
func walkID3v2(r io.ReaderAt, offset, available int64, visit func(version byte, id string, data []byte)) error {
	if available < 10 {
		return errInvalidTags
	}
	var h [10]byte
	if _, err := r.ReadAt(h[:], offset); err != nil {
		return err
	}
	if string(h[:3]) != "ID3" || (h[3] != 2 && h[3] != 3 && h[3] != 4) {
		return errInvalidTags
	}
	version := h[3]
	n, ok := synchsafeSize(h[6:10])
	footer := int64(0)
	if version == 4 && h[5]&0x10 != 0 {
		footer = 10
	}
	if !ok || n > maxTagBytes || n > available-10-footer {
		return errInvalidTags
	}
	body := make([]byte, int(n))
	if n > 0 {
		if _, err := r.ReadAt(body, offset+10); err != nil {
			return err
		}
	}
	if version == 3 && h[5]&0x80 != 0 {
		body = removeUnsynchronisation(body)
	}
	if h[5]&0x40 != 0 {
		switch version {
		case 3:
			if len(body) < 4 {
				return errInvalidTags
			}
			ext := int64(binary.BigEndian.Uint32(body[:4]))
			if ext < 4 || ext > int64(len(body)-4) {
				return errInvalidTags
			}
			body = body[4+ext:]
		case 4:
			if len(body) < 4 {
				return errInvalidTags
			}
			ext, valid := synchsafeSize(body[:4])
			if !valid || ext < 4 || ext > int64(len(body)) {
				return errInvalidTags
			}
			body = body[ext:]
		default:
			return errInvalidTags
		}
	}
	headerSize := 10
	if version == 2 {
		headerSize = 6
	}
	idSize := 4
	if version == 2 {
		idSize = 3
	}
	// Some DSF taggers insert one stray byte before the first frame.
	if len(body) > idSize+1 && body[0] != 0 && id3Field(version, string(body[1:1+idSize])) != "" {
		body = body[1:]
	}
	for i := 0; i < maxTagEntries && len(body) > 0; i++ {
		if len(body) < headerSize {
			return errInvalidTags
		}
		padding := true
		for _, c := range body {
			if c != 0 {
				padding = false
				break
			}
		}
		if padding {
			break
		}
		id := string(body[:3])
		if version != 2 {
			id = string(body[:4])
		}
		for _, c := range []byte(id) {
			if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return errInvalidTags
			}
		}
		var frameLen int64
		var flags uint16
		if version == 2 {
			frameLen = int64(body[3])<<16 | int64(body[4])<<8 | int64(body[5])
		} else if version == 3 {
			frameLen = int64(binary.BigEndian.Uint32(body[4:8]))
			flags = binary.BigEndian.Uint16(body[8:10])
		} else {
			var valid bool
			frameLen, valid = synchsafeSize(body[4:8])
			if !valid {
				return errInvalidTags
			}
			flags = binary.BigEndian.Uint16(body[8:10])
		}
		body = body[headerSize:]
		if frameLen > int64(len(body)) {
			return errInvalidTags
		}
		data := body[:int(frameLen)]
		body = body[int(frameLen):]
		if version == 4 && flags&0x000c != 0 || version != 4 && flags&0x00c0 != 0 || version == 2 && flags&0x4000 != 0 {
			continue
		}
		if version == 4 {
			if flags&0x0002 != 0 {
				data = removeUnsynchronisation(data)
			}
			if flags&0x0040 != 0 {
				if len(data) < 1 {
					return errInvalidTags
				}
				data = data[1:]
			}
			if flags&0x0001 != 0 {
				if len(data) < 4 {
					return errInvalidTags
				}
				dl, valid := synchsafeSize(data[:4])
				if !valid || dl > int64(len(data)-4) {
					return errInvalidTags
				}
				data = data[4 : 4+int(dl)]
			}
		} else if flags&0x0020 != 0 && len(data) > 0 {
			data = data[1:]
		}
		visit(version, id, data)
	}
	return nil
}

func readID3v1(r io.ReaderAt, size int64, encoding string) Tags {
	var tags Tags
	if size < 128 {
		return tags
	}
	var b [128]byte
	if _, err := r.ReadAt(b[:], size-128); err != nil || string(b[:3]) != "TAG" {
		return tags
	}
	text := func(start, end int) string {
		return decodeLegacyTagText(bytes.Trim(b[start:end], "\x00 "), encoding)
	}
	tags.set("TITLE", text(3, 33))
	tags.set("ARTIST", text(33, 63))
	tags.set("ALBUM", text(63, 93))
	tags.set("YEAR", text(93, 97))
	if b[125] != 0 && b[126] != 0 {
		tags.set("TRACKNUMBER", strconv.Itoa(int(b[125])))
	}
	if int(b[127]) < len(id3v1Genres) {
		tags.set("GENRE", id3v1Genres[b[127]])
	}
	return tags
}

var id3v1Genres = [...]string{
	"Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk", "Grunge", "Hip-Hop", "Jazz", "Metal",
	"New Age", "Oldies", "Other", "Pop", "R&B", "Rap", "Reggae", "Rock", "Techno", "Industrial",
	"Alternative", "Ska", "Death Metal", "Pranks", "Soundtrack", "Euro-Techno", "Ambient", "Trip-Hop", "Vocal", "Jazz+Funk",
	"Fusion", "Trance", "Classical", "Instrumental", "Acid", "House", "Game", "Sound Clip", "Gospel", "Noise",
	"AlternRock", "Bass", "Soul", "Punk", "Space", "Meditative", "Instrumental Pop", "Instrumental Rock", "Ethnic", "Gothic",
	"Darkwave", "Techno-Industrial", "Electronic", "Pop-Folk", "Underground", "Jungle", "Native American", "Cabaret", "New Wave", "Psychedelic",
	"Rave", "Showtunes", "Lo-Fi", "Tribal", "Acid Punk", "Acid Jazz", "Polka", "Retro", "Musical", "Rock & Roll",
	"Hard Rock", "Folk", "Folk-Rock", "National Folk", "Swing", "Fast Fusion", "Bebob", "Latin", "Revival", "Celtic",
	"Bluegrass", "Avantgarde", "Gothic Rock", "Progressive Rock", "Psychedelic Rock", "Symphonic Rock", "Slow Rock", "Big Band", "Chorus", "Easy Listening",
	"Acoustic", "Humour", "Speech", "Chanson", "Opera", "Chamber Music", "Sonata", "Symphony", "Booty Bass", "Primus",
	"Porn Groove", "Satire", "Slow Jam", "Club", "Tango", "Samba", "Folklore", "Ballad", "Power Ballad", "Freestyle",
}
