package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServesIndex(t *testing.T) {
	h := Handler()
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Zephyr Music Server") || !strings.Contains(body, "/app.js") {
		t.Fatalf("index shell missing expected markers")
	}
}

func TestServesAssets(t *testing.T) {
	h := Handler()
	for _, p := range []string{"/style.css", "/app.js"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", p, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s empty body", p)
		}
	}
	css := get(t, h, "/style.css")
	if !strings.Contains(css.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css content-type = %q", css.Header().Get("Content-Type"))
	}
	js := get(t, h, "/app.js")
	if !strings.Contains(js.Body.String(), "/api/v1") {
		t.Fatalf("app.js missing real API wiring")
	}
}

func TestUnknownPathFallsBackToShell(t *testing.T) {
	h := Handler()
	rec := get(t, h, "/some/deep/link")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SPA fallback)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Zephyr Music Server") {
		t.Fatalf("fallback did not serve index shell")
	}
}