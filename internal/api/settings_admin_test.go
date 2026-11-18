package api

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/config"
)

const adminTestPassword = "test-only-password-123456"
const adminTestDisplayName = "Test Administrator"
const adminTestSetupBody = `{"password":"` + adminTestPassword + `","display_name":"` + adminTestDisplayName + `"}`

func adminTestAPI(t *testing.T) *API {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir()}
	return &API{cfg: cfg, admin: newSettingsAdmin(cfg)}
}

func adminTestRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "http://localhost/api/v1/settings/session", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:24680"
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func adminTestSetup(t *testing.T, a *API) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	a.setupSettingsAdmin(w, adminTestRequest("POST", adminTestSetupBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	return cookies[0]
}

func TestSettingsAdminSetupBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, remote, host, origin, contentType string
		tls                                     bool
		want                                    int
	}{
		{"remote", "192.0.2.1:12", "localhost", "http://localhost", "application/json", false, 403},
		{"rebinding", "127.0.0.1:12", "attacker.example", "http://attacker.example", "application/json", false, 403},
		{"remote-tls-setup", "192.0.2.1:12", "localhost", "https://localhost", "application/json", true, 403},
		{"cross-origin", "127.0.0.1:12", "localhost", "http://attacker.example", "application/json", false, 403},
		{"missing-origin", "127.0.0.1:12", "localhost", "", "application/json", false, 403},
		{"null-origin", "127.0.0.1:12", "localhost", "null", "application/json", false, 403},
		{"form", "127.0.0.1:12", "localhost", "http://localhost", "text/plain", false, 415},
		{"ipv6", "[::1]:12", "[::1]:8080", "http://[::1]:8080", "application/json", false, 201},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := adminTestAPI(t)
			r := adminTestRequest("POST", adminTestSetupBody)
			r.RemoteAddr, r.Host = test.remote, test.host
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Content-Type", test.contentType)
			if test.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			a.setupSettingsAdmin(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if test.want != 201 && a.admin.configured {
				t.Fatal("rejected setup configured an account")
			}
		})
	}
}

func TestSettingsAdminSessionLifecycle(t *testing.T) {
	a := adminTestAPI(t)
	cookie := adminTestSetup(t, a)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/api/v1" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if _, ok := a.admin.sessions[sha256.Sum256([]byte(cookie.Value))]; !ok {
		t.Fatal("token not stored as digest")
	}
	req := adminTestRequest("GET", "")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.settingsSession(w, req)
	var status map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["authenticated"] != true || status["configured"] != true || status["can_setup"] != false || len(status) != 4 {
		t.Fatalf("status: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), a.cfg.DataDir) || strings.Contains(w.Body.String(), adminTestPassword) {
		t.Fatal("status leak")
	}
	called := false
	guard := a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
	w = httptest.NewRecorder()
	guard(w, req)
	if w.Code != 204 || !called {
		t.Fatal("valid session rejected")
	}
	logout := adminTestRequest("POST", `{}`)
	logout.AddCookie(cookie)
	w = httptest.NewRecorder()
	a.logoutSettingsAdmin(w, logout)
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	called = false
	w = httptest.NewRecorder()
	guard(w, req)
	if w.Code != 401 || called {
		t.Fatal("logged-out token accepted")
	}

	login := adminTestRequest("POST", `{"password":"`+adminTestPassword+`"}`)
	login.RemoteAddr = "192.0.2.8:1000"
	login.TLS = &tls.ConnectionState{}
	login.Host = "music.example"
	login.Header.Set("Origin", "https://music.example")
	w = httptest.NewRecorder()
	a.loginSettingsAdmin(w, login)
	if w.Code != 200 || !w.Result().Cookies()[0].Secure {
		t.Fatalf("HTTPS login: %d", w.Code)
	}
	cookie = w.Result().Cookies()[0]
	now := time.Now().Add(settingsSessionTTL + time.Minute)
	a.admin.now = func() time.Time { return now }
	expired := adminTestRequest("GET", "")
	expired.AddCookie(cookie)
	w = httptest.NewRecorder()
	guard(w, expired)
	if w.Code != 401 || len(a.admin.sessions) != 0 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("expired token accepted or retained")
	}
}

func TestSettingsAdminPersistenceAndThrottle(t *testing.T) {
	a := adminTestAPI(t)
	cookie := adminTestSetup(t, a)
	path := filepath.Join(a.cfg.DataDir, "settings-admin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), adminTestPassword) || strings.Contains(string(data), cookie.Value) {
		t.Fatal("plaintext credential persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("credential file mode")
	}
	a.admin = newSettingsAdmin(a.cfg)
	if !a.admin.configured || a.admin.unavailable || len(a.admin.sessions) != 0 {
		t.Fatal("credential reload failed")
	}
	w := httptest.NewRecorder()
	a.loginSettingsAdmin(w, adminTestRequest("POST", `{"password":"incorrect-long-password"}`))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	a.loginSettingsAdmin(w, adminTestRequest("POST", `{"password":"`+adminTestPassword+`"}`))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("missing throttle")
	}
	a.admin.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	w = httptest.NewRecorder()
	a.loginSettingsAdmin(w, adminTestRequest("POST", `{"password":"`+adminTestPassword+`"}`))
	if w.Code != 200 {
		t.Fatalf("login after backoff: %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.setupSettingsAdmin(w, adminTestRequest("POST", `{"password":"another-long-password","display_name":"Another"}`))
	if w.Code != 409 {
		t.Fatal("setup overwrote password")
	}
}

func TestSettingsAdminMutationGuard(t *testing.T) {
	a := adminTestAPI(t)
	guard := a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	w := httptest.NewRecorder()
	guard(w, adminTestRequest("POST", `{}`))
	if w.Code != 401 {
		t.Fatalf("unconfigured allowed: %d", w.Code)
	}
	cookie := adminTestSetup(t, a)
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		want   int
	}{
		{"unauthenticated", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"cross-origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"form", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415},
		{"remote-http", func(r *http.Request) { r.RemoteAddr = "192.0.2.4:90"; r.Header.Set("X-Forwarded-Proto", "https") }, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := adminTestRequest("POST", `{}`)
			r.AddCookie(cookie)
			test.change(r)
			w := httptest.NewRecorder()
			guard(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
}

func TestSettingsAdminEmptyDirAndInvalidPassword(t *testing.T) {
	a := &API{admin: newSettingsAdmin(config.Config{})}
	w := httptest.NewRecorder()
	a.setupSettingsAdmin(w, adminTestRequest("POST", adminTestSetupBody))
	if w.Code != 503 {
		t.Fatalf("empty dir write allowed: %d", w.Code)
	}
	a = adminTestAPI(t)
	for _, body := range []string{`{"password":"short","display_name":"Admin"}`, `{"password":"` + adminTestPassword + `","display_name":"Admin","extra":true}`, adminTestSetupBody + ` {}`, `null`} {
		w = httptest.NewRecorder()
		a.setupSettingsAdmin(w, adminTestRequest("POST", body))
		if w.Code != 400 {
			t.Fatalf("invalid password JSON: %d", w.Code)
		}
	}
	if err := os.WriteFile(filepath.Join(a.cfg.DataDir, "settings-admin.json"), []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	a.admin = newSettingsAdmin(a.cfg)
	w = httptest.NewRecorder()
	a.setupSettingsAdmin(w, adminTestRequest("POST", adminTestSetupBody))
	if w.Code != 409 {
		t.Fatal("corrupt credential file allowed reset")
	}
}
