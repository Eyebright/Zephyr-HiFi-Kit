//go:build windows

package media

import "testing"

func TestDecodeLegacyTagEncoding(t *testing.T) {
	gbk := []byte{0xD6, 0xD0, 0xCE, 0xC4, 0xB8, 0xE8, 0xC7, 0xFA} // 中文歌曲
	if got := decodeTagText(gbk, EncodingAuto); got != "中文歌曲" {
		t.Fatalf("auto GBK = %q", got)
	}
	if got := decodeTagText(gbk, EncodingGBK); got != "中文歌曲" {
		t.Fatalf("manual GBK = %q", got)
	}
	if got := decodeTagText([]byte("纯正 UTF-8"), EncodingGBK); got != "纯正 UTF-8" {
		t.Fatalf("valid UTF-8 must not be converted: %q", got)
	}
}
