package serverclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/nodes/register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["token"] != "good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(Node{NodeID: "n-123", Alias: body["alias"], Arch: body["arch"]})
	})
	mux.HandleFunc("GET /api/v1/apps/shairport-sync/versions", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]AppVersion{
			{AppID: "shairport-sync", Version: "4.3.1", Arch: "arm64", PublishedAt: 100, SHA256: "old"},
			{AppID: "shairport-sync", Version: "4.3.2", Arch: "arm64", PublishedAt: 200, SHA256: "new"},
			{AppID: "shairport-sync", Version: "4.3.3", Arch: "arm64", PublishedAt: 300, Banned: true},
			{AppID: "shairport-sync", Version: "4.3.2", Arch: "amd64", PublishedAt: 250},
		})
	})
	mux.HandleFunc("GET /api/v1/repo/shairport-sync/4.3.2/arm64", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("artifact-bytes"))
	})
	mux.HandleFunc("POST /api/v1/jobs/job-1/phases", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(Job{ID: "job-1", State: body["phase"]})
	})
	return httptest.NewServer(mux)
}

func TestRegister(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	c := New(srv.URL)
	n, err := c.Register(context.Background(), "good-token", "living-pi", "arm64", "debian13", "6.6", "mid")
	if err != nil || n.NodeID != "n-123" {
		t.Fatalf("register: %v %+v", err, n)
	}
	if _, err := c.Register(context.Background(), "bad", "x", "arm64", "", "", ""); err == nil {
		t.Fatal("bad token must fail")
	}
}

func TestLatestSkipsBannedAndWrongArch(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	c := New(srv.URL)
	v, err := c.Latest(context.Background(), "shairport-sync", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "4.3.2" || v.Arch != "arm64" {
		t.Fatalf("latest should be 4.3.2 arm64 (4.3.3 banned), got %+v", v)
	}
}

func TestDownloadHash(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	c := New(srv.URL)
	dest := filepath.Join(t.TempDir(), "a.deb")
	sum, err := c.Download(context.Background(), "shairport-sync", "4.3.2", "arm64", dest)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("artifact-bytes"))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("hash=%s", sum)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "artifact-bytes" {
		t.Fatalf("dest content=%q", b)
	}
}

func TestReportPhase(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	c := New(srv.URL)
	j, err := c.ReportPhase(context.Background(), "job-1", "installing", "ok", "")
	if err != nil || j.State != "installing" {
		t.Fatalf("phase: %v %+v", err, j)
	}
}

func TestDownloadDist(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dl/hifihub-agent_0.4.0_amd64.deb", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("agent-deb-bytes"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL)
	dest := filepath.Join(t.TempDir(), "updates", "a.deb")
	sum, err := c.DownloadDist(context.Background(), "/dl/hifihub-agent_0.4.0_amd64.deb", dest)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("agent-deb-bytes"))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("hash=%s", sum)
	}
	if b, _ := os.ReadFile(dest); string(b) != "agent-deb-bytes" {
		t.Fatalf("dest=%q", b)
	}
	if _, err := c.DownloadDist(context.Background(), "dl/relative.deb", dest); err == nil {
		t.Fatal("relative path must be rejected")
	}
	if _, err := c.DownloadDist(context.Background(), "/dl/../secret", dest); err == nil {
		t.Fatal("traversal path must be rejected")
	}
	if _, err := c.DownloadDist(context.Background(), "/dl/missing.deb", dest); err == nil {
		t.Fatal("404 must surface as error")
	}
}
