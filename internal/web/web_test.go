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
	if !strings.Contains(body, "Zephyr HiFi Kit") || !strings.Contains(body, "和风HiFi音乐播放套件") || !strings.Contains(body, "/app.js") {
		t.Fatalf("index shell missing expected markers")
	}
	previous := -1
	for _, script := range []string{"i18n.js", "i18n-app.js", "i18n-settings.js", "i18n-features.js", "i18n-errors.js", "app.js", "library-browser.js", "settings.js", "experience.js", "favorites.js", "terminal-center.js", "terminal-install.js"} {
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
	for _, p := range []string{"/i18n.js", "/i18n-app.js", "/i18n-settings.js", "/i18n-features.js", "/i18n-errors.js", "/style.css", "/app.js", "/library-browser.js", "/library-browser.css", "/settings.js", "/settings.css", "/experience.js", "/experience.css", "/favorites.js", "/favorites.css", "/terminal-center.js", "/terminal-center.css", "/terminal-install.js"} {
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

func TestConsoleResourcesRevalidateAfterUpgrade(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/app.js", "/i18n.js", "/i18n-app.js", "/some/deep/link"} {
		if got := get(t, h, path).Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s cache-control = %q", path, got)
		}
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

func TestAssetETagReturnsNotModified(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	tag := rec.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing ETag")
	}
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-None-Match", tag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation status = %d", rec.Code)
	}
}

func TestConsoleSecurityHeaders(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/app.js", "/no-shell-route"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s nosniff: %q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("%s frame options: %q", path, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Fatalf("%s csp: %q", path, got)
		}
	}
}
