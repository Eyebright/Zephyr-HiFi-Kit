package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.4.0", "0.3.0", 1},
		{"0.3.0", "0.4.0", -1},
		{"0.3.0", "0.3.0", 0},
		{"v0.4.0", "0.3.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		{"0.3.1", "0.3.0", 1},
		{"0.3", "0.3.0", -1},
		{"0.50", "0.4.3", 1},
		{"0.4.3", "0.50", -1},
		{"v0.50", "0.50", 0},
		{"0.51", "0.50", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestValidVersion(t *testing.T) {
	ok := []string{"0.50", "0.4.0", "1.2.3.4", "1.0.0", "0.4.0-rc1", "10.20.30"}
	bad := []string{"", "../etc", "0.4.0/../../x", "abc", "1.2;rm", "1.2.3.4.5"}
	for _, v := range ok {
		if !ValidVersion(v) {
			t.Errorf("ValidVersion(%q)=false want true", v)
		}
	}
	for _, v := range bad {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q)=true want false", v)
		}
	}
}

func TestSameAssetOrigin(t *testing.T) {
	cases := []struct {
		feed, asset string
		want        bool
	}{
		{"api.github.com", "github.com", true},
		{"api.github.com", "objects.githubusercontent.com", true},
		{"api.github.com", "release-assets.githubusercontent.com", true},
		{"api.github.com", "evil.com", false},
		{"api.github.com", "github.com.evil.com", false},
		{"github.com", "api.github.com", true},
		{"mirror.internal", "mirror.internal", true},
		{"mirror.internal", "other.internal", false},
		{"127.0.0.1", "127.0.0.1", true},
	}
	for _, c := range cases {
		if got := sameAssetOrigin(c.feed, c.asset); got != c.want {
			t.Errorf("sameAssetOrigin(%q,%q)=%v want %v", c.feed, c.asset, got, c.want)
		}
	}
}

func TestValidateAssetURL(t *testing.T) {
	cases := []struct {
		feed, asset string
		wantErr     bool
	}{
		{"https://api.github.com/repos/x/releases/latest", "https://github.com/x/releases/download/v1/a.tar.gz", false},
		{"https://api.github.com/repos/x/releases/latest", "https://objects.githubusercontent.com/a.tar.gz", false},
		{"https://api.github.com/repos/x/releases/latest", "https://evil.com/a.tar.gz", true},
		{"https://api.github.com/repos/x/releases/latest", "https://169.254.169.254/latest/meta-data", true},
		{"https://api.github.com/repos/x/releases/latest", "http://github.com/a.tar.gz", true},
		{"https://mirror.internal/feed", "https://other.internal/a.tar.gz", true},
		{"http://127.0.0.1:8080/feed", "http://127.0.0.1:8080/a.tar.gz", false},
		{"https://api.github.com/feed", "ftp://github.com/a", true},
	}
	for _, c := range cases {
		err := validateAssetURL(c.feed, c.asset)
		if (err != nil) != c.wantErr {
			t.Errorf("validateAssetURL(%q,%q) err=%v wantErr=%v", c.feed, c.asset, err, c.wantErr)
		}
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestHTTFeedLatest(t *testing.T) {
	body := []byte(`{"tag_name":"v0.4.0","assets":[{"name":"a.tar.gz","browser_download_url":"https://github.com/x/a.tar.gz","digest":"sha256:abc","size":10}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	rel, err := NewFeed(srv.URL).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version() != "0.4.0" {
		t.Fatalf("version=%q", rel.Version())
	}
	a, ok := rel.Asset("a.tar.gz")
	if !ok || a.Digest != "sha256:abc" || a.URL != "https://github.com/x/a.tar.gz" {
		t.Fatalf("asset=%+v ok=%v", a, ok)
	}
	if _, ok := rel.Asset("missing"); ok {
		t.Fatal("missing asset reported present")
	}
}

func TestHTTFeedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewFeed(srv.URL).Latest(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assets":[]}`))
	}))
	defer empty.Close()
	if _, err := NewFeed(empty.URL).Latest(context.Background()); err == nil {
		t.Fatal("expected error on empty tag")
	}
}

func TestFetchAssetDigest(t *testing.T) {
	payload := []byte("release-bundle-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "a.tar.gz")
	a := Asset{Name: "a.tar.gz", URL: srv.URL + "/a.tar.gz", Digest: "sha256:" + sha(payload)}
	got, err := fetchAsset(context.Background(), srv.URL+"/feed", a, dest)
	if err != nil {
		t.Fatal(err)
	}
	if got != sha(payload) {
		t.Fatalf("sum=%s", got)
	}
	b, _ := os.ReadFile(dest)
	if !bytes.Equal(b, payload) {
		t.Fatal("dest content mismatch")
	}
	bad := Asset{Name: "a.tar.gz", URL: srv.URL + "/a.tar.gz", Digest: "sha256:deadbeef"}
	if _, err := fetchAsset(context.Background(), srv.URL+"/feed", bad, dest); err == nil {
		t.Fatal("expected digest mismatch error")
	}
	cross := Asset{Name: "x", URL: "https://evil.com/x"}
	if _, err := fetchAsset(context.Background(), srv.URL+"/feed", cross, dest); err == nil {
		t.Fatal("expected cross-origin rejection")
	}
}

// tarGz builds a gzipped tar from entries; a nil content marks a directory.
func tarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.flag}
		switch e.flag {
		case tar.TypeDir:
			hdr.Mode = 0o755
		case tar.TypeSymlink:
			hdr.Linkname = e.link
		default:
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.flag == tar.TypeReg {
			if _, err := tw.Write(e.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name    string
	content []byte
	flag    byte
	link    string
}

func reg(name, content string) tarEntry {
	return tarEntry{name: name, content: []byte(content), flag: tar.TypeReg}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "b.tar.gz")
	data := tarGz(t, []tarEntry{
		{name: "root/", flag: tar.TypeDir},
		reg("root/a.txt", "A"),
		{name: "root/sub/", flag: tar.TypeDir},
		reg("root/sub/b.txt", "B"),
	})
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x")
	if err := extractTarGz(src, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "root", "a.txt")); string(b) != "A" {
		t.Fatal("a.txt missing")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "root", "sub", "b.txt")); string(b) != "B" {
		t.Fatal("b.txt missing")
	}
}

func TestExtractTarGzRejectsUnsafe(t *testing.T) {
	for _, e := range []tarEntry{
		reg("../evil.txt", "x"),
		reg("/abs/evil.txt", "x"),
		{name: "root/link", flag: tar.TypeSymlink, link: "/etc/passwd"},
	} {
		dir := t.TempDir()
		src := filepath.Join(dir, "b.tar.gz")
		if err := os.WriteFile(src, tarGz(t, []tarEntry{e}), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := extractTarGz(src, filepath.Join(dir, "x")); err == nil {
			t.Fatalf("expected rejection for %+v", e)
		}
	}
}

func TestVerifySums(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := []byte("alpha")
	b := []byte("beta")
	_ = os.WriteFile(filepath.Join(dir, "server", "a"), a, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b"), b, 0o644)
	sums := filepath.Join(dir, "SHA256SUMS")
	good := fmt.Sprintf("%s  server/a\n%s  b\n", sha(a), sha(b))
	if err := os.WriteFile(sums, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySums(dir, sums); err != nil {
		t.Fatalf("good sums: %v", err)
	}
	bad := fmt.Sprintf("%s  server/a\n%s  b\n", sha(a), sha([]byte("wrong")))
	_ = os.WriteFile(sums, []byte(bad), 0o644)
	if err := verifySums(dir, sums); err == nil {
		t.Fatal("expected mismatch")
	}
	_ = os.WriteFile(sums, []byte(""), 0o644)
	if err := verifySums(dir, sums); err == nil {
		t.Fatal("expected empty error")
	}
	escape := fmt.Sprintf("%s  ../outside\n", sha(a))
	_ = os.WriteFile(sums, []byte(escape), 0o644)
	if err := verifySums(dir, sums); err == nil {
		t.Fatal("expected escape rejection")
	}
}

func TestReleaseVersionAndAsset(t *testing.T) {
	r := Release{Tag: "v1.2.3", Assets: []Asset{{Name: "x"}, {Name: "y"}}}
	if r.Version() != "1.2.3" {
		t.Fatal("version strip")
	}
	if _, ok := r.Asset("y"); !ok {
		t.Fatal("asset y")
	}
	if _, ok := r.Asset("z"); ok {
		t.Fatal("asset z should be absent")
	}
}

func TestReadDistSums(t *testing.T) {
	dir := t.TempDir()
	deb := "hifihub-agent_0.4.0_amd64.deb"
	content := fmt.Sprintf("%s  %s\n%s  agent-install.sh\n", sha([]byte("d")), deb, sha([]byte("s")))
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := readDistSums(dir)
	if m[deb] != sha([]byte("d")) {
		t.Fatalf("deb sum=%q", m[deb])
	}
	if len(readDistSums("")) != 0 {
		t.Fatal("empty dir should yield empty map")
	}
	if len(readDistSums(filepath.Join(dir, "nope"))) != 0 {
		t.Fatal("missing dir should yield empty map")
	}
}

func TestTailLog(t *testing.T) {
	if tailLog("") != "终端上报失败" {
		t.Fatal("empty tail")
	}
	long := strings.Repeat("x", 300)
	got := tailLog(long)
	if len([]rune(got)) != 241 || !strings.HasPrefix(got, "…") {
		t.Fatalf("tail not truncated: runes=%d", len([]rune(got)))
	}
}

func TestBackoffSeconds(t *testing.T) {
	if backoffSeconds(0) != 0 || backoffSeconds(1) != 60 || backoffSeconds(3) != 180 {
		t.Fatal("backoff")
	}
}

// jsonFeedHandler serves a GitHub-shaped release JSON pointing at assetSrv.
func jsonFeedHandler(tag, assetName, assetURL, digest string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gr := map[string]any{
			"tag_name": tag,
			"assets": []map[string]any{
				{"name": assetName, "browser_download_url": assetURL, "digest": digest, "size": 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gr)
	}
}
