package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/store"
)

const settingsLanguageTestPath = "/api/v1/settings/language"

func languageTestAPI(t *testing.T, persistent bool) (*API, *store.Store) {
	t.Helper()
	var a *API
	var st *store.Store
	if persistent {
		a, st = profileTestAPI(t)
	} else {
		a = adminTestAPI(t)
	}
	return New(a.cfg, Deps{Library: a.lib}), st
}

func languageTestRequest(method, body string, cookie *http.Cookie) *http.Request {
	r := adminTestRequest(method, body)
	r.URL.Path = settingsLanguageTestPath
	r.RequestURI = r.URL.RequestURI()
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func languageTestCall(a *API, method, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, languageTestRequest(method, body, cookie))
	return w
}

func assertLanguageResponse(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var fields map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || w.Code != http.StatusOK || len(fields) != 1 || fields["default_language"] != want || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("language response: %d %s cache=%q, want %q", w.Code, w.Body.String(), w.Header().Get("Cache-Control"), want)
	}
}

func TestSettingsLanguagePublicDefault(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			a, _ := languageTestAPI(t, persistent)
			r := httptest.NewRequest("GET", "http://music.example"+settingsLanguageTestPath, nil)
			r.RemoteAddr = "192.0.2.3:1234"
			// Browser preferences must not silently change the server default.
			r.Header.Set("Accept-Language", "en,zh-TW;q=0.9")
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			assertLanguageResponse(t, w, "zh-CN")
			if w := languageTestCall(a, "PATCH", `{"default_language":"en"}`, nil); w.Code != http.StatusUnauthorized {
				t.Fatalf("unconfigured PATCH allowed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSettingsLanguagePersistenceAndProfileIsolation(t *testing.T) {
	a, st := languageTestAPI(t, true)
	cookie := adminTestSetup(t, a)
	before := profileTestGet(t, a)
	for _, language := range []string{"en", "zh-CN", "zh-TW"} {
		w := languageTestCall(a, "PATCH", `{"default_language":"`+language+`"}`, cookie)
		assertLanguageResponse(t, w, language)
		assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), language)
		if got := profileTestGet(t, a); got != before {
			t.Fatalf("language changed four-field profile: %+v", got)
		}
	}
	a, _ = profileTestRestart(t, a, st)
	a = New(a.cfg, Deps{Library: a.lib})
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
	if w := languageTestCall(a, "PATCH", `{"default_language":"en"}`, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("restart kept an old administrator session", w.Code)
	}
	cookie = profileTestLogin(t, a)
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Changed"}`, cookie); w.Code != http.StatusOK {
		t.Fatal("profile update failed", w.Code)
	}
	if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":true}`, cookie); w.Code != http.StatusOK {
		t.Fatal("onboarding update failed", w.Code)
	}
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
	assertLanguageResponse(t, languageTestCall(a, "PATCH", `{"default_language":"en"}`, cookie), "en")
	if got := profileTestGet(t, a); got.DisplayName != "Changed" || got.OnboardingStep != 3 || !got.OnboardingComplete || got.Legacy {
		t.Fatalf("language overwrote updated profile: %+v", got)
	}
}

func TestSettingsLanguageNilLibraryIsolation(t *testing.T) {
	a, _ := languageTestAPI(t, false)
	cookie := adminTestSetup(t, a)
	before := profileTestGet(t, a)
	assertLanguageResponse(t, languageTestCall(a, "PATCH", `{"default_language":"zh-TW"}`, cookie), "zh-TW")
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
	if got := profileTestGet(t, a); got != before {
		t.Fatalf("memory language changed profile: %+v", got)
	}
	other := New(a.cfg, Deps{})
	assertLanguageResponse(t, languageTestCall(other, "GET", "", nil), "zh-CN")
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Memory profile"}`, cookie); w.Code != http.StatusOK {
		t.Fatal("memory profile update failed", w.Code)
	}
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
}

func TestSettingsLanguageMutationSecurity(t *testing.T) {
	a, _ := languageTestAPI(t, true)
	cookie := adminTestSetup(t, a)
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
		want   int
	}{
		{"anonymous", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"invalid-session", func(r *http.Request) { r.Header.Set("Cookie", settingsCookie+"=invalid") }, 401},
		{"cross-origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403},
		{"missing-origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"null-origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"same-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
		{"remote-http", func(r *http.Request) {
			r.RemoteAddr = "192.0.2.4:1234"
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("Forwarded", "for=127.0.0.1;proto=https")
		}, 403},
		{"loopback-rebinding", func(r *http.Request) {
			r.Host = "evil.example"
			r.Header.Set("Origin", "http://evil.example")
		}, 403},
		{"form", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := languageTestRequest("PATCH", `{"default_language":"en"}`, cookie)
			tc.change(r)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("mutation guard: %d %s", w.Code, w.Body.String())
			}
			assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-CN")
		})
	}
	// A real same-origin HTTPS request from the LAN is allowed.
	r := languageTestRequest("PATCH", `{"default_language":"en"}`, cookie)
	r.TLS = &tls.ConnectionState{}
	r.RemoteAddr, r.Host = "192.0.2.4:1234", "music.example"
	r.Header.Set("Origin", "https://music.example")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	assertLanguageResponse(t, w, "en")
	a.admin.now = func() time.Time { return time.Now().Add(settingsSessionTTL + time.Minute) }
	if w := languageTestCall(a, "PATCH", `{"default_language":"zh-TW"}`, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("expired session updated language", w.Code)
	}
	assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "en")
}

func TestSettingsLanguageStrictValidation(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			a, _ := languageTestAPI(t, persistent)
			cookie := adminTestSetup(t, a)
			assertLanguageResponse(t, languageTestCall(a, "PATCH", `{"default_language":"zh-TW"}`, cookie), "zh-TW")
			for _, body := range []string{
				``, `null`, `{}`, `[]`, `"en"`, `{"default_language":null}`, `{"default_language":1}`, `{"default_language":true}`,
				`{"default_language":""}`, `{"default_language":"fr"}`, `{"default_language":"en-US"}`, `{"default_language":"zh-cn"}`,
				`{"default_language":"EN"}`, `{"default_language":" en "}`, `{"default_language":"en\u0000"}`,
				`{"default_language":"en","language":"zh-CN"}`, `{"default_language":"en","display_name":"Injected"}`,
				`{"default_language":"en","extra":true}`, `{"default_language":"en","default_language":"zh-CN"}`,
				`{"DEFAULT_LANGUAGE":"en"}`, `{"default_language":"en"`, `{"default_language":"en"} {}`, `{"default_language":"en"} trailing`,
				`{"default_language":"` + string([]byte{0xff}) + `"}`, `{"default_language":"` + strings.Repeat("a", 8192) + `"}`,
			} {
				w := languageTestCall(a, "PATCH", body, cookie)
				if w.Code != http.StatusBadRequest || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("invalid language JSON accepted: %d %s for %.100q", w.Code, w.Body.String(), body)
				}
				assertLanguageResponse(t, languageTestCall(a, "GET", "", nil), "zh-TW")
			}
			if got := profileTestGet(t, a); got.DisplayName != adminTestDisplayName || got.OnboardingStep != 2 || got.OnboardingComplete {
				t.Fatalf("invalid request changed profile: %+v", got)
			}
		})
	}
}

func TestSettingsLanguageStorageFailures(t *testing.T) {
	for _, mode := range []string{"corrupt", "closed", "missing-table", "insert", "update"} {
		t.Run(mode, func(t *testing.T) {
			a, st := languageTestAPI(t, true)
			cookie := adminTestSetup(t, a)
			// Ensure failures cannot leak or replace the real value with a memory fallback.
			a.admin.defaultLanguage = "en"
			want := "zh-CN"
			switch mode {
			case "corrupt":
				if _, err := st.DB.Exec(`INSERT INTO server_state(key,value) VALUES('settings.language.v1','synthetic-corrupt-value')`); err != nil {
					t.Fatal(err)
				}
			case "closed":
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing-table":
				if _, err := st.DB.Exec(`DROP TABLE server_state`); err != nil {
					t.Fatal(err)
				}
			case "insert":
				if _, err := st.DB.Exec(`CREATE TRIGGER reject_language_insert BEFORE INSERT ON server_state
					WHEN NEW.key='settings.language.v1' BEGIN SELECT RAISE(ABORT, 'synthetic insert failure'); END`); err != nil {
					t.Fatal(err)
				}
			case "update":
				assertLanguageResponse(t, languageTestCall(a, "PATCH", `{"default_language":"zh-TW"}`, cookie), "zh-TW")
				want = "zh-TW"
				if _, err := st.DB.Exec(`CREATE TRIGGER reject_language_update BEFORE UPDATE ON server_state
					WHEN NEW.key='settings.language.v1' AND NEW.value<>OLD.value
					BEGIN SELECT RAISE(ABORT, 'synthetic update failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			w := languageTestCall(a, "PATCH", `{"default_language":"en"}`, cookie)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "synthetic") {
				t.Fatalf("storage failure hidden/leaked: %d %s", w.Code, w.Body.String())
			}
			w = languageTestCall(a, "GET", "", nil)
			if mode == "corrupt" || mode == "closed" || mode == "missing-table" {
				if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "synthetic") {
					t.Fatalf("read failure returned defaults/leaked: %d %s", w.Code, w.Body.String())
				}
			} else {
				assertLanguageResponse(t, w, want)
			}
			if mode == "corrupt" {
				var value string
				if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key='settings.language.v1'`).Scan(&value); err != nil || value != "synthetic-corrupt-value" {
					t.Fatalf("corrupt state overwritten: %q %v", value, err)
				}
				if got := profileTestGet(t, a); got.DisplayName != adminTestDisplayName {
					t.Fatal("language corruption broke profile", got)
				}
			}
		})
	}
}
