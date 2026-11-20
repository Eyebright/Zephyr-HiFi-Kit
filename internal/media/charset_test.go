package media

import "testing"

func TestNormalizeTextEncoding(t *testing.T) {
	cases := map[string]string{
		"":             EncodingAuto,
		"auto":         EncodingAuto,
		"UTF-8":        EncodingUTF8,
		"gb2312":       EncodingGBK,
		"CP950":        EncodingBig5,
		"shift-jis":    EncodingShiftJIS,
		"windows-1251": EncodingCP1251,
	}
	for in, want := range cases {
		got, ok := NormalizeTextEncoding(in)
		if !ok || got != want {
			t.Fatalf("NormalizeTextEncoding(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := NormalizeTextEncoding("koi8-r"); ok {
		t.Fatal("unsupported encoding accepted")
	}
}

func TestTextScorePrefersReadableText(t *testing.T) {
	if textScore("中文歌曲") <= textScore("\uFFFD\uFFFD\uFFFD") {
		t.Fatal("readable CJK text must outrank replacement characters")
	}
	if textScore("Hello") <= 0 {
		t.Fatal("ASCII text should score positively")
	}
}
