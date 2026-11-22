package remote

import (
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"simple":       "'simple'",
		"a'b":          `'a'\''b'`,
		"http://x/y?z": "'http://x/y?z'",
		"":             "''",
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactAndTail(t *testing.T) {
	tok := strings.Repeat("ab", 24)
	out := "echo " + tok + " done"
	if got := redact(out, tok); strings.Contains(got, tok) {
		t.Fatalf("token not redacted: %q", got)
	}
	long := strings.Repeat("x", 3000) + " end"
	got := tail(long, tok)
	if len(got) > 1600 || !strings.HasSuffix(got, "end") {
		t.Fatalf("tail wrong length %d", len(got))
	}
	if tail("  \n", tok) != "" {
		t.Fatal("blank output should produce empty detail")
	}
}

func TestTargetAuth(t *testing.T) {
	if _, err := dial(t.Context(), Target{Host: "127.0.0.1", Port: 1, User: "u"}, nil); err == nil || !strings.Contains(err.Error(), "密码或私钥") {
		t.Fatalf("expected credential error, got %v", err)
	}
	_, err := dial(t.Context(), Target{Host: "127.0.0.1", Port: 1, User: "u", PrivateKey: "not-a-key"}, nil)
	if err == nil || !strings.Contains(err.Error(), "私钥解析失败") {
		t.Fatalf("expected key parse error, got %v", err)
	}
}
