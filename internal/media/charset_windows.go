//go:build windows

package media

import (
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

var windowsCodePages = map[string]uint32{
	EncodingGBK:      936,
	EncodingBig5:     950,
	EncodingShiftJIS: 932,
	EncodingCP1251:   1251,
}

// MB_ERR_INVALID_CHARS makes the conversion fail instead of substituting.
const mbErrInvalidChars = 0x00000008

func decodeWithEncoding(b []byte, enc string) (string, bool) {
	if enc == EncodingUTF8 {
		if utf8.Valid(b) {
			return string(b), true
		}
		return "", false
	}
	cp, ok := windowsCodePages[enc]
	if !ok || len(b) == 0 {
		return "", false
	}
	n, err := windows.MultiByteToWideChar(cp, mbErrInvalidChars, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return "", false
	}
	buf := make([]uint16, n)
	n2, err := windows.MultiByteToWideChar(cp, mbErrInvalidChars, &b[0], int32(len(b)), &buf[0], n)
	if err != nil || n2 != n {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}
