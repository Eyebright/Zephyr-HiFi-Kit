package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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
	if !strings.Contains(body, "Zephyr HiFi Kit") || !strings.Contains(body, "/app.js") {
		t.Fatalf("index shell missing expected markers")
	}
	previous := -1
	for _, script := range []string{"app.js", "library-browser.js", "settings.js", "experience.js", "favorites.js", "terminal-center.js", "terminal-install.js"} {
		marker := `<script src="/` + script + `"></script>`
		at := strings.Index(body, marker)
		if at <= previous || strings.Count(body, marker) != 1 {
			t.Fatalf("script %s missing, duplicated, or out of dependency order", script)
		}
		previous = at
	}
	if !strings.Contains(body, `<link rel="stylesheet" href="/terminal-center.css">`) || strings.Contains(body, "服务下发") {
		t.Fatal("index must load terminal-center styles without the retired delivery description")
	}
}

func TestIndexHasGlobalPlayerBarRoot(t *testing.T) {
	body := get(t, Handler(), "/").Body.String()
	for _, id := range []string{"view", "player-bar-root"} {
		if count := strings.Count(body, `id="`+id+`"`); count != 1 {
			t.Fatalf("index has %d %s roots, want exactly one", count, id)
		}
	}
	// Replacing the view must not remove playback controls.
	siblings := regexp.MustCompile(`<main\b[^>]*\bid="view"[^>]*>\s*</main>\s*<div\b[^>]*\bid="player-bar-root"[^>]*>\s*</div>`)
	if !siblings.MatchString(body) {
		t.Fatal("player-bar-root must be outside and immediately beside the empty view root")
	}
	if strings.Contains(body, `id="player-bar"`) || strings.Contains(body, `/player-bar.js`) || strings.Contains(body, `/player-bar.css`) {
		t.Fatal("index must mount one player via the existing app assets, not duplicate its shell or add an asset")
	}
}

func TestServesAssets(t *testing.T) {
	h := Handler()
	for _, p := range []string{"/style.css", "/app.js", "/library-browser.js", "/library-browser.css", "/settings.js", "/settings.css", "/experience.js", "/experience.css", "/favorites.js", "/favorites.css", "/terminal-center.js", "/terminal-center.css", "/terminal-install.js"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", p, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s empty body", p)
		}
		ct := rec.Header().Get("Content-Type")
		if (strings.HasSuffix(p, ".css") && !strings.Contains(ct, "text/css")) ||
			(strings.HasSuffix(p, ".js") && !strings.Contains(ct, "javascript")) {
			t.Fatalf("%s content-type = %q, expected asset rather than SPA fallback", p, ct)
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
	if !strings.Contains(rec.Body.String(), "Zephyr HiFi Kit") {
		t.Fatalf("fallback did not serve index shell")
	}
}
