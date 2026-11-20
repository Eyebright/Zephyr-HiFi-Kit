//go:build !windows

package media

import "unicode/utf8"

// Non-Windows builds keep UTF-8 tags and pass through legacy encodings only
// when a transcoder is available (not bundled).
func decodeWithEncoding(b []byte, enc string) (string, bool) {
	if enc == EncodingUTF8 || enc == "" {
		if utf8.Valid(b) {
			return string(b), true
		}
	}
	return "", false
}
