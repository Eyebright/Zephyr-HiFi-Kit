package androidcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/agent/internal/serverclient"
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
	for path, want := range map[string]string{
		"/":            "Zephyr HiFi Kit",
		"/console.css": ".queue-pane",
		"/console.js":  "/api/queue",
	} {
		rec := httptest.NewRecorder()
		Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("GET %s missing %q", path, want)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q", path, got)
		}
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/console.js", nil))
	if !strings.Contains(rec.Body.String(), "/api/lyrics") {
		t.Fatal("console.js missing lyrics API")
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

func TestConsoleQueueRoutes(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		var body map[string]int
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/nodes/n-123/queue":
			if body != nil {
				t.Fatalf("unexpected queue request body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(serverclient.QueueState{
				NodeID: "n-123",
				Tracks: []serverclient.QueueTrack{{TrackID: "t-1", Title: "Track One"}},
			})
		case "/api/v1/nodes/n-123/queue/prev", "/api/v1/nodes/n-123/queue/next":
		case "/api/v1/nodes/n-123/queue/jump":
			if body["index"] != 2 {
				t.Fatalf("jump index = %d, want 2", body["index"])
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newTestCore(t)
	c.cfg.ServerURL = srv.URL
	c.client = serverclient.New(srv.URL)
	if err := c.st.SetNode("n-123", "test", "android"); err != nil {
		t.Fatal(err)
	}
	h := Handler(c)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/queue", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/queue = %d: %s", rec.Code, rec.Body.String())
	}
	var queue serverclient.QueueState
	if err := json.Unmarshal(rec.Body.Bytes(), &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Tracks) != 1 || queue.Tracks[0].Title != "Track One" {
		t.Fatalf("queue = %+v", queue)
	}

	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/prev", ""},
		{"/api/next", ""},
		{"/api/jump", `{"index":2}`},
	} {
		rec = httptest.NewRecorder()
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s = %d: %s", tc.path, rec.Code, rec.Body.String())
		}
	}
	want := []string{
		"GET /api/v1/nodes/n-123/queue",
		"POST /api/v1/nodes/n-123/queue/prev",
		"POST /api/v1/nodes/n-123/queue/next",
		"POST /api/v1/nodes/n-123/queue/jump",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %v, want %v", calls, want)
	}
}

func TestConsoleLyrics(t *testing.T) {
	c := newTestCore(t)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/lyrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/lyrics = %d: %s", rec.Code, rec.Body.String())
	}
	var empty serverclient.Lyrics
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil || empty.Source != "none" || len(empty.Lines) != 0 {
		t.Fatalf("idle lyrics = %+v, %v", empty, err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tracks/trk-1/lyrics" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(serverclient.Lyrics{
			TrackID: "trk-1", Source: "sidecar", Synced: true,
			Lines: []serverclient.LyricsLine{{TimeMS: 1000, Text: "你好"}},
		})
	}))
	defer srv.Close()
	got, err := serverclient.New(srv.URL).TrackLyrics(context.Background(), "trk-1")
	if err != nil || got.TrackID != "trk-1" || !got.Synced || len(got.Lines) != 1 || got.Lines[0].Text != "你好" {
		t.Fatalf("TrackLyrics = %+v, %v", got, err)
	}
}

func TestConsoleRejectsInvalidInput(t *testing.T) {
	c := newTestCore(t)
	h := Handler(c)
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/volume", `{"volume":-1}`},
		{"/api/volume", `{"volume":101}`},
		{"/api/volume", `{"volume":`},
		{"/api/jump", `{"index":-1}`},
		{"/api/jump", `{"index":`},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %s %s = %d, want 400", tc.path, tc.body, rec.Code)
		}
	}
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{"POST", "/api/state", http.StatusMethodNotAllowed},
		{"POST", "/api/queue", http.StatusMethodNotAllowed},
		{"GET", "/api/pause", http.StatusMethodNotAllowed},
		{"GET", "/api/stop", http.StatusMethodNotAllowed},
		{"GET", "/api/seek", http.StatusMethodNotAllowed},
		{"GET", "/api/volume", http.StatusMethodNotAllowed},
		{"GET", "/api/prev", http.StatusMethodNotAllowed},
		{"GET", "/api/next", http.StatusMethodNotAllowed},
		{"GET", "/api/jump", http.StatusMethodNotAllowed},
		{"POST", "/", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
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
