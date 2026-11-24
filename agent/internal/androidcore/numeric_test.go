package androidcore

import "testing"

func TestNumericCode(t *testing.T) {
	for _, ok := range []string{"123456", "1234567", "12345678"} {
		if !numericCode(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "12345", "123456789", "12345a", "abcdef12"} {
		if numericCode(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}