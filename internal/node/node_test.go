package node

import (
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, 45*time.Second)
}

func TestIssueNumericTokenDigitsAndUniqueness(t *testing.T) {
	s := newSvc(t)
	seen := map[string]bool{}
	for _, digits := range []int{6, 7, 8} {
		tok, exp, err := s.IssueNumericToken(time.Minute, digits)
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != digits || !IsNumericCode(tok) {
			t.Fatalf("digits=%d token=%q not numeric of requested length", digits, tok)
		}
		if exp <= time.Now().Unix() {
			t.Fatalf("expiry not in future")
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}
		seen[tok] = true
	}
	// Out-of-range requests clamp into 6..8.
	for _, digits := range []int{0, 2, 99} {
		tok, _, err := s.IssueNumericToken(time.Minute, digits)
		if err != nil || !IsNumericCode(tok) {
			t.Fatalf("clamp digits=%d tok=%q err=%v", digits, tok, err)
		}
	}
}

func TestIsNumericCode(t *testing.T) {
	for _, ok := range []string{"123456", "1234567", "12345678"} {
		if !IsNumericCode(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "12345", "123456789", "12345a", "abcdef", "12 456"} {
		if IsNumericCode(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestRegisterAndroidRequiresNumericCode(t *testing.T) {
	s := newSvc(t)
	hexTok, _, err := s.IssueToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(RegisterRequest{Token: hexTok, Alias: "a", Arch: "android"}, "127.0.0.1"); err == nil {
		t.Fatal("android register accepted hex token")
	}
	numTok, _, err := s.IssueNumericToken(time.Minute, 8)
	if err != nil {
		t.Fatal(err)
	}
	n, secret, err := s.Register(RegisterRequest{Token: numTok, Alias: "phone", Arch: "android", Distro: "android"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("android numeric register: %v", err)
	}
	if n.NodeID == "" || secret == "" {
		t.Fatalf("register returned empty identity")
	}
	// Linux keeps hex tokens.
	linuxTok, _, err := s.IssueToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(RegisterRequest{Token: linuxTok, Alias: "pi", Arch: "arm64"}, "127.0.0.1"); err != nil {
		t.Fatalf("linux hex register: %v", err)
	}
}