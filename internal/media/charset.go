package media

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text encodings accepted by music sources. Auto tries the legacy candidates
// only when the bytes are not valid UTF-8.
const (
	EncodingAuto     = "auto"
	EncodingUTF8     = "utf-8"
	EncodingGBK      = "gbk"
	EncodingBig5     = "big5"
	EncodingShiftJIS = "shift_jis"
	EncodingCP1251   = "windows-1251"
)

var legacyEncodings = []string{EncodingGBK, EncodingBig5, EncodingShiftJIS, EncodingCP1251}

// NormalizeTextEncoding validates a source setting, mapping "" to auto.
func NormalizeTextEncoding(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "auto":
		return EncodingAuto, true
	case "utf-8", "utf8":
		return EncodingUTF8, true
	case "gbk", "gb2312", "gb18030":
		return EncodingGBK, true
	case "big5", "cp950":
		return EncodingBig5, true
	case "shift-jis", "shift_jis", "sjis", "cp932":
		return EncodingShiftJIS, true
	case "windows-1251", "cp1251":
		return EncodingCP1251, true
	default:
		return "", false
	}
}

func decodeTagText(b []byte, encoding string) string {
	b = bytes.Trim(b, "\x00")
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
	if enc == EncodingAuto {
		return autoDecode(b)
	}
	if s, ok := decodeWithEncoding(b, enc); ok {
		return s
	}
	return ""
}

func autoDecode(b []byte) string {
	best, bestScore := "", -1<<30
	for _, enc := range legacyEncodings {
		s, ok := decodeWithEncoding(b, enc)
		if !ok {
			continue
		}
		if score := textScore(s); score > bestScore {
			best, bestScore = s, score
		}
	}
	if bestScore < 0 {
		return ""
	}
	return best
}

// textScore prefers plausible text: CJK/Cyrillic add weight, replacement and
// control characters subtract. Pure ASCII decodes equally under any candidate.
func textScore(s string) int {
	score, runes := 0, 0
	for _, r := range s {
		runes++
		switch {
		case r == utf8.RuneError || r == '\uFFFD':
			score -= 50
		case unicode.IsControl(r):
			score -= 10
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			score += 6
		case unicode.Is(unicode.Cyrillic, r):
			score += 3
		case r >= 0x20 && r <= 0x7e:
			score++
		}
	}
	if runes == 0 {
		return -1 << 20
	}
	return score
}
