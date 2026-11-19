package androidcore

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentMachineID(t *testing.T) {
	dir := t.TempDir()
	a := persistentMachineID(dir)
	b := persistentMachineID(dir)
	if a == "" || a != b {
		t.Fatalf("machine id not stable: %q vs %q", a, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "machine-id")); err != nil {
		t.Fatalf("machine-id not persisted: %v", err)
	}
}

func newTestCore(t *testing.T) *Core {
	t.Helper()
	c, err := New(Config{ServerURL: "http://127.0.0.1:1", DataDir: t.TempDir(), Alias: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewDefaults(t *testing.T) {
	c := newTestCore(t)
	if c.arch != "android" {
		t.Fatalf("arch = %q, want android", c.arch)
	}
	if c.ConsoleAddr() != "127.0.0.1:8090" {
		t.Fatalf("console addr = %q", c.ConsoleAddr())
	}
	if c.cfg.ZATAddr != "127.0.0.1:7800" {
		t.Fatalf("derived ZAT addr = %q", c.cfg.ZATAddr)
	}
}

func TestConsoleServesHTML(t *testing.T) {
	c := newTestCore(t)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET / = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Zephyr HiFi Kit") {
		t.Fatal("console HTML missing brand")
	}
}

func TestConsoleVolume(t *testing.T) {
	c := newTestCore(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/volume", strings.NewReader(`{"volume":50}`))
	Handler(c).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST /api/volume = %d: %s", rec.Code, rec.Body.String())
	}
	if c.Playback().Volume != 50 {
		t.Fatalf("volume = %d, want 50", c.Playback().Volume)
	}
}

func TestConsoleUnknownPath(t *testing.T) {
	c := newTestCore(t)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("GET /nope = %d, want 404", rec.Code)
	}
}
