package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hifihub/server/internal/audit"
	"hifihub/server/internal/library"
	"hifihub/server/internal/store"
)

func profileTestAPI(t *testing.T) (*API, *store.Store) {
	t.Helper()
	a := adminTestAPI(t)
	st, err := store.Open(filepath.Join(a.cfg.DataDir, "profile-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a.lib = library.New(st, nil, "")
	return a, st
}

func profileTestCall(h http.HandlerFunc, method, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := adminTestRequest(method, body)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func profileTestGet(t *testing.T, a *API) library.SettingsProfile {
	t.Helper()
	w := profileTestCall(a.getSettingsProfile, "GET", "", nil)
	var p library.SettingsProfile
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatalf("get profile: %d %s", w.Code, w.Body.String())
	}
	var fields map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || len(fields) != 4 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("profile response shape/cache: %s", w.Body.String())
	}
	return p
}

func profileTestLogin(t *testing.T, a *API) *http.Cookie {
	t.Helper()
	w := profileTestCall(a.loginSettingsAdmin, "POST", `{"password":"`+adminTestPassword+`"}`, nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("password-only login: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func profileTestRestart(t *testing.T, a *API, st *store.Store) (*API, *store.Store) {
	t.Helper()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(a.cfg.DataDir, "profile-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a = &API{cfg: a.cfg, admin: newSettingsAdmin(a.cfg), lib: library.New(st, nil, "")}
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	return a, st
}

func TestSettingsProfileNewInstanceRestartsAndCompletes(t *testing.T) {
	a, st := profileTestAPI(t)
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	if got := profileTestGet(t, a); got != library.InitialSettingsProfile(false) {
		t.Fatalf("new profile: %+v", got)
	}
	a, st = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got != library.InitialSettingsProfile(false) {
		t.Fatalf("new restart: %+v", got)
	}
	w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":true}`, nil)
	if w.Code != 401 {
		t.Fatal("advanced before credential creation", w.Code)
	}
	cookie := adminTestSetup(t, a)
	want := library.SettingsProfile{DisplayName: adminTestDisplayName, OnboardingStep: 2}
	if got := profileTestGet(t, a); got != want {
		t.Fatalf("setup profile: %+v", got)
	}
	// Only synthetic credentials under t.TempDir are read by these tests.
	data, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "settings-admin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var credentials map[string]json.RawMessage
	if err := json.Unmarshal(data, &credentials); err != nil || len(credentials) != 4 || credentials["version"] == nil || credentials["iterations"] == nil || credentials["salt"] == nil || credentials["hash"] == nil {
		t.Fatalf("credentials schema changed: %s %v", data, err)
	}
	if strings.Contains(string(data), adminTestDisplayName) || strings.Contains(string(data), adminTestPassword) {
		t.Fatal("profile or plaintext password stored in credentials")
	}
	a, st = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got != want {
		t.Fatalf("setup restart lost onboarding: %+v", got)
	}
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Other"}`, cookie); w.Code != 401 {
		t.Fatal("restart retained a session", w.Code)
	}
	cookie = profileTestLogin(t, a)
	for _, body := range []string{`{"step":3,"complete":false}`, `{"step":1,"complete":false}`, `{"step":3,"complete":true}`, `{"step":2,"complete":false}`, `{"step":3,"complete":true}`} {
		if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", body, cookie); w.Code != 200 {
			t.Fatalf("onboarding progression/revisit: %d %s", w.Code, w.Body.String())
		}
	}
	a, _ = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got.OnboardingStep != 3 || !got.OnboardingComplete || got.Legacy || got.DisplayName != adminTestDisplayName {
		t.Fatalf("completed restart: %+v", got)
	}
}

func TestSettingsProfileLegacyAndPublicGreeting(t *testing.T) {
	a, st := profileTestAPI(t)
	// Create old-format credentials without a persistent profile, as an older
	// server would. The nil-library compatibility path stores no profile file.
	lib := a.lib
	a.lib = nil
	adminTestSetup(t, a)
	a = &API{cfg: a.cfg, admin: newSettingsAdmin(a.cfg), lib: lib}
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	want := library.InitialSettingsProfile(true)
	if got := profileTestGet(t, a); got != want {
		t.Fatalf("legacy profile forced onboarding: %+v", got)
	}
	cookie := profileTestLogin(t, a)
	if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":true}`, cookie); w.Code != 400 {
		t.Fatal("legacy account advanced with no name", w.Code)
	}
	name := `<script>alert('x')</script>`
	body, err := json.Marshal(map[string]string{"display_name": " \u3000" + name + " "})
	if err != nil {
		t.Fatal(err)
	}
	w := profileTestCall(a.updateSettingsProfile, "PATCH", string(body), cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), `\u003cscript\u003e`) {
		t.Fatalf("name JSON was not HTML-escaped: %d %s", w.Code, w.Body.String())
	}
	want.DisplayName = name
	if got := profileTestGet(t, a); got != want {
		t.Fatalf("name update changed progress/legacy: %+v", got)
	}
	// Public LAN greeting: no login or privileged transport needed for GET.
	r := httptest.NewRequest("GET", "http://music.example/api/v1/settings/profile", nil)
	r.RemoteAddr = "192.0.2.3:1234"
	w = httptest.NewRecorder()
	a.getSettingsProfile(w, r)
	if w.Code != 200 {
		t.Fatal("public greeting requires login", w.Code)
	}
	a, _ = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got != want {
		t.Fatalf("escaped name not preserved: %+v", got)
	}
}

func TestSettingsProfileMutationSecurity(t *testing.T) {
	a, _ := profileTestAPI(t)
	for _, h := range []http.HandlerFunc{a.updateSettingsProfile, a.updateSettingsOnboarding} {
		if w := profileTestCall(h, "PATCH", `{}`, nil); w.Code != 401 {
			t.Fatal("unconfigured mutation allowed", w.Code)
		}
	}
	cookie := adminTestSetup(t, a)
	before := profileTestGet(t, a)
	for _, h := range []http.HandlerFunc{a.updateSettingsProfile, a.updateSettingsOnboarding} {
		for _, test := range []struct {
			name   string
			change func(*http.Request)
			want   int
		}{
			{"unauthorized", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
			{"cross-origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
			{"no-origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
			{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
			{"http-remote", func(r *http.Request) { r.RemoteAddr = "192.0.2.3:1234"; r.Header.Set("X-Forwarded-Proto", "https") }, 403},
			{"form", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		} {
			t.Run(test.name, func(t *testing.T) {
				r := adminTestRequest("PATCH", `{}`)
				r.AddCookie(cookie)
				test.change(r)
				w := httptest.NewRecorder()
				h(w, r)
				if w.Code != test.want || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("guard: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
	if got := profileTestGet(t, a); got != before {
		t.Fatal("rejected requests changed profile", got)
	}
}

func TestSettingsProfileStrictValidation(t *testing.T) {
	a, _ := profileTestAPI(t)
	cookie := adminTestSetup(t, a)
	for _, body := range []string{`null`, `{}`, `{"display_name":null}`, `{"display_name":1}`, `{"display_name":"A","display_name":"B"}`, `{"DISPLAY_NAME":"A"}`, `{"display_name":"A"`, `{"display_name":""}`, `{"display_name":"  "}`, `{"display_name":"A\u0000"}`, `{"display_name":"\nA"}`, `{"display_name":"A","extra":true}`, `{"display_name":"A"} {}`, `{"display_name":"` + strings.Repeat("界", 41) + `"}`, `{"display_name":"` + string([]byte{0xff}) + `"}`, `{"display_name":"` + strings.Repeat("A", 8192) + `"}`} {
		if w := profileTestCall(a.updateSettingsProfile, "PATCH", body, cookie); w.Code != 400 {
			t.Fatalf("invalid profile accepted: %d %s", w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`null`, `{}`, `{"step":2}`, `{"step":2,"step":4,"complete":true}`, `{"Step":2,"complete":false}`, `{"step":2,"complete":null}`, `{"step":null,"complete":false}`, `{"step":0,"complete":false}`, `{"step":5,"complete":false}`, `{"step":4,"complete":false}`, `{"step":1.5,"complete":false}`, `{"step":"2","complete":false}`, `{"step":2,"complete":"false"}`, `{"step":2,"complete":true}`, `{"step":4,"complete":true,"extra":1}`, `{"step":3,"complete":true} {}`} {
		if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", body, cookie); w.Code != 400 {
			t.Fatalf("invalid onboarding accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if got := profileTestGet(t, a); got.DisplayName != adminTestDisplayName || got.OnboardingStep != 2 || got.OnboardingComplete {
		t.Fatal("invalid requests changed state", got)
	}
	for _, name := range []string{"A", strings.Repeat("界", 40)} {
		if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":" `+name+` "}`, cookie); w.Code != 200 {
			t.Fatal("valid Unicode name rejected", w.Code)
		}
	}
	if w := profileTestCall(a.loginSettingsAdmin, "POST", adminTestSetupBody, nil); w.Code != 400 {
		t.Fatal("login no longer password-only", w.Code)
	}
}

func TestSettingsSetupRequiresValidName(t *testing.T) {
	a, _ := profileTestAPI(t)
	for _, body := range []string{
		`{"password":"` + adminTestPassword + `"}`,
		`{"password":"` + adminTestPassword + `","display_name":null}`,
		`{"password":"` + adminTestPassword + `","password":"another-password","display_name":"A"}`,
		`{"password":"` + adminTestPassword + `","display_name":"A","unknown":true}`,
		`{"password":"` + adminTestPassword + `","display_name":" "}`,
		`{"password":"` + adminTestPassword + `","display_name":"A\t"}`,
		`{"password":"` + adminTestPassword + `","display_name":"` + strings.Repeat("界", 41) + `"}`,
		`{"password":"` + adminTestPassword + `","display_name":"` + string([]byte{0xff}) + `"}`,
		`{"password":"12345678901","display_name":"A"}`,
		`{"password":"` + strings.Repeat("A", 1025) + `","display_name":"A"}`,
		adminTestSetupBody + ` {}`,
	} {
		if w := profileTestCall(a.setupSettingsAdmin, "POST", body, nil); w.Code != 400 {
			t.Fatalf("invalid setup accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(a.cfg.DataDir, "settings-admin.json")); !os.IsNotExist(err) {
		t.Fatal("invalid setup created credentials", err)
	}
	w := profileTestCall(a.setupSettingsAdmin, "POST", `{"password":"十二个汉字密码仅供测试使用","display_name":"  管理员  "}`, nil)
	if w.Code != 201 {
		t.Fatalf("valid Unicode setup rejected: %d %s", w.Code, w.Body.String())
	}
	var response map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response) != 2 || !response["configured"] || !response["authenticated"] {
		t.Fatal("setup response contract changed", response, err)
	}
	if p := profileTestGet(t, a); p.DisplayName != "管理员" || p.OnboardingStep != 2 {
		t.Fatal("setup name not normalized", p)
	}
}

func TestSettingsProfileInitialWriteFailureBlocksCredentials(t *testing.T) {
	a, st := profileTestAPI(t)
	if _, err := st.DB.Exec(`CREATE TRIGGER reject_profile_insert BEFORE INSERT ON server_state
		WHEN NEW.key='settings.profile.v1' BEGIN SELECT RAISE(ABORT, 'synthetic insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSettingsProfile(); err == nil {
		t.Fatal("initialization ignored persistence failure")
	}
	if w := profileTestCall(a.getSettingsProfile, "GET", "", nil); w.Code != 503 {
		t.Fatal("failed initialization returned defaults", w.Code)
	}
	w := profileTestCall(a.settingsSession, "GET", "", nil)
	if !strings.Contains(w.Body.String(), `"can_setup":false`) {
		t.Fatal("session advertises unusable setup", w.Body.String())
	}
	w = profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil)
	if w.Code != 503 || a.admin.configured || strings.Contains(w.Body.String(), "synthetic") || strings.Contains(w.Body.String(), adminTestPassword) {
		t.Fatalf("initial write failed open: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.cfg.DataDir, "settings-admin.json")); !os.IsNotExist(err) {
		t.Fatal("created credentials before marker", err)
	}
	if _, err := st.DB.Exec(`DROP TRIGGER reject_profile_insert`); err != nil {
		t.Fatal(err)
	}
	adminTestSetup(t, a)
}

func TestSettingsProfileFailureAfterCredentialsCanLoginAndResume(t *testing.T) {
	a, st := profileTestAPI(t)
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`CREATE TRIGGER reject_profile_update BEFORE UPDATE ON server_state
		WHEN NEW.key='settings.profile.v1' AND NEW.value<>OLD.value
		BEGIN SELECT RAISE(ABORT, 'synthetic update failure'); END`); err != nil {
		t.Fatal(err)
	}
	w := profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "管理员已创建") || !a.admin.configured || a.admin.unavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("partial setup result: %d %s", w.Code, w.Body.String())
	}
	w = profileTestCall(a.settingsSession, "GET", "", nil)
	if !strings.Contains(w.Body.String(), `"configured":true`) || !strings.Contains(w.Body.String(), `"can_setup":false`) {
		t.Fatal("partial setup advertised retry", w.Body.String())
	}
	if w := profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil); w.Code != 409 {
		t.Fatal("partial setup allowed credential overwrite", w.Code)
	}
	a, st = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got != library.InitialSettingsProfile(false) {
		t.Fatal("partial setup misclassified as legacy", got)
	}
	cookie := profileTestLogin(t, a)
	if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":true}`, cookie); w.Code != 400 {
		t.Fatal("partial setup skipped missing name", w.Code)
	}
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Recovered"}`, cookie); w.Code != 503 {
		t.Fatal("write failure returned success", w.Code)
	}
	if _, err := st.DB.Exec(`DROP TRIGGER reject_profile_update`); err != nil {
		t.Fatal(err)
	}
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Recovered"}`, cookie); w.Code != 200 {
		t.Fatal("cannot repair name", w.Code)
	}
	if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":2,"complete":false}`, cookie); w.Code != 200 {
		t.Fatal("cannot resume onboarding", w.Code)
	}
	a, _ = profileTestRestart(t, a, st)
	if got := profileTestGet(t, a); got.DisplayName != "Recovered" || got.OnboardingStep != 2 || got.OnboardingComplete || got.Legacy {
		t.Fatal("recovered progress not durable", got)
	}
}

func TestSettingsProfileCorruptionAndUnavailableCredentials(t *testing.T) {
	t.Run("corrupt-profile", func(t *testing.T) {
		a, st := profileTestAPI(t)
		if _, err := st.DB.Exec(`INSERT INTO server_state(key,value) VALUES('settings.profile.v1','broken')`); err != nil {
			t.Fatal(err)
		}
		if err := a.initializeSettingsProfile(); err == nil {
			t.Fatal("corrupt profile initialized")
		}
		if w := profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil); w.Code != 503 || a.admin.configured {
			t.Fatal("corrupt profile allowed credentials", w.Code)
		}
		if w := profileTestCall(a.getSettingsProfile, "GET", "", nil); w.Code != 503 {
			t.Fatal("corruption hidden", w.Code)
		}
	})
	for _, reload := range []bool{false, true} {
		t.Run(map[bool]string{false: "exclusive-existing", true: "corrupt-credentials-restart"}[reload], func(t *testing.T) {
			a, _ := profileTestAPI(t)
			path := filepath.Join(a.cfg.DataDir, "settings-admin.json")
			if err := os.WriteFile(path, []byte("synthetic-corrupt-credentials"), 0600); err != nil {
				t.Fatal(err)
			}
			if reload {
				a.admin = newSettingsAdmin(a.cfg)
			}
			if w := profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil); w.Code != 409 || !a.admin.configured || !a.admin.unavailable {
				t.Fatal("corrupt credentials allowed setup", w.Code)
			}
			if w := profileTestCall(a.settingsSession, "GET", "", nil); !strings.Contains(w.Body.String(), `"can_setup":false`) {
				t.Fatal("setup reopened after corruption", w.Body.String())
			}
			if w := profileTestCall(a.loginSettingsAdmin, "POST", `{"password":"`+adminTestPassword+`"}`, nil); w.Code != 401 {
				t.Fatal("corrupt credentials allowed login", w.Code)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "synthetic-corrupt-credentials" {
				t.Fatal("existing credentials overwritten", err)
			}
		})
	}
}

func TestSettingsProfileNilLibraryCompatibility(t *testing.T) {
	a := adminTestAPI(t)
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	if got := profileTestGet(t, a); got != library.InitialSettingsProfile(false) {
		t.Fatal("nil library defaults", got)
	}
	cookie := adminTestSetup(t, a)
	if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"In memory"}`, cookie); w.Code != 200 {
		t.Fatal("nil library rename", w.Code)
	}
	if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":false}`, cookie); w.Code != 200 {
		t.Fatal("nil library progress", w.Code)
	}
	if err := a.initializeSettingsProfile(); err != nil {
		t.Fatal(err)
	}
	if got := profileTestGet(t, a); got.DisplayName != "In memory" || got.OnboardingStep != 3 || got.OnboardingComplete || got.Legacy {
		t.Fatal("nil library reinitialized profile", got)
	}
}

func TestSettingsProfileRuntimeStorageFailureDoesNotBlockLogin(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrupt", true: "missing"}[remove], func(t *testing.T) {
			a, st := profileTestAPI(t)
			adminTestSetup(t, a)
			query := `UPDATE server_state SET value='broken' WHERE key='settings.profile.v1'`
			if remove {
				query = `DELETE FROM server_state WHERE key='settings.profile.v1'`
			}
			if _, err := st.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
			cookie := profileTestLogin(t, a)
			if w := profileTestCall(a.getSettingsProfile, "GET", "", nil); w.Code != 503 {
				t.Fatal("runtime storage failure hidden", w.Code)
			}
			if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Other"}`, cookie); w.Code != 503 {
				t.Fatal("storage error used memory fallback", w.Code)
			}
			if err := a.initializeSettingsProfile(); err == nil {
				t.Fatal("runtime corruption replaced with legacy defaults")
			}
			if w := profileTestCall(a.settingsSession, "GET", "", cookie); !strings.Contains(w.Body.String(), `"authenticated":true`) || !strings.Contains(w.Body.String(), `"can_setup":false`) {
				t.Fatal("profile failure broke credential state", w.Body.String())
			}
		})
	}
}

func TestSettingsProfileSetupAuditDoesNotContainSecrets(t *testing.T) {
	a, st := profileTestAPI(t)
	a.aud = audit.New(st)
	w := profileTestCall(a.withAudit(a.setupSettingsAdmin), "POST", adminTestSetupBody, nil)
	if w.Code != 201 {
		t.Fatal("audited setup failed", w.Code)
	}
	entries, err := a.aud.List(10)
	if err != nil || len(entries) != 1 {
		t.Fatal("missing audit", err)
	}
	data, err := json.Marshal(entries)
	if err != nil || strings.Contains(string(data), adminTestPassword) || strings.Contains(string(data), w.Result().Cookies()[0].Value) || strings.Contains(string(data), adminTestDisplayName) {
		t.Fatal("request body or token leaked to audit", err)
	}
}

func TestSettingsProfileConcurrentSetupAndUpdates(t *testing.T) {
	a, _ := profileTestAPI(t)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { <-start; results <- profileTestCall(a.setupSettingsAdmin, "POST", adminTestSetupBody, nil) }()
	}
	close(start)
	var cookie *http.Cookie
	codes := map[int]int{}
	for range 2 {
		w := <-results
		codes[w.Code]++
		if w.Code == 201 {
			cookie = w.Result().Cookies()[0]
		}
	}
	if codes[201] != 1 || codes[409] != 1 {
		t.Fatal("setup not serialized", codes)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 3)
	for _, run := range []func(){
		func() {
			if w := profileTestCall(a.updateSettingsProfile, "PATCH", `{"display_name":"Concurrent"}`, cookie); w.Code != 200 {
				errs <- w.Body.String()
			}
		},
		func() {
			if w := profileTestCall(a.updateSettingsOnboarding, "PATCH", `{"step":3,"complete":true}`, cookie); w.Code != 200 {
				errs <- w.Body.String()
			}
		},
		func() {
			if err := a.initializeSettingsProfile(); err != nil {
				errs <- err.Error()
			}
		},
	} {
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if p := profileTestGet(t, a); p.DisplayName != "Concurrent" || p.OnboardingStep != 3 || !p.OnboardingComplete || p.Legacy {
		t.Fatal("concurrent operation lost fields", p)
	}
}
