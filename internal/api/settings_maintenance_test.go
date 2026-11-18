package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/library"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
	"hifihub/server/internal/zatserver"
)

func maintenanceTestAPI(t *testing.T) (*API, *store.Store, *http.Cookie) {
	t.Helper()
	a := adminTestAPI(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "api-maintenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a.lib = library.New(st, nil, t.TempDir())
	a.nodes = node.New(st, time.Minute)
	a.zat = zatserver.New("127.0.0.1:0", a.nodes, a.lib, st) // Never Listen or Serve.
	return a, st, adminTestSetup(t, a)
}

func TestSettingsMaintenanceAuthentication(t *testing.T) {
	a := adminTestAPI(t)
	for _, h := range []http.HandlerFunc{a.settingsMaintenance, a.exportSettingsBackup, a.restoreSettingsBackup, a.cleanupSettingsLibrary} {
		w := httptest.NewRecorder()
		h(w, adminTestRequest("POST", `{}`))
		if w.Code != 401 {
			t.Fatalf("unconfigured operation: %d %s", w.Code, w.Body.String())
		}
	}
	cookie := adminTestSetup(t, a)
	for _, h := range []http.HandlerFunc{a.settingsMaintenance, a.exportSettingsBackup, a.restoreSettingsBackup, a.cleanupSettingsLibrary} {
		r := adminTestRequest("POST", `{}`)
		r.AddCookie(cookie)
		r.Header.Set("Origin", "https://attacker.example")
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin operation: %d", w.Code)
		}
	}
}

func TestSettingsMaintenanceBackupRestoreAndCleanup(t *testing.T) {
	a, st, cookie := maintenanceTestAPI(t)
	m, err := a.lib.AddMount(library.AddMountRequest{Host: "nas", Share: "music", Username: "synthetic-user", Password: "synthetic-password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO tracks(id,mount_id,path,rel_path,title,scanned_at) VALUES('t',?,?,'file.flac','Title',1)`, m.ID, filepath.Join(m.Target, "file.flac")); err != nil {
		t.Fatal(err)
	}
	call := func(h http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
		r := adminTestRequest(method, body)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	w := call(a.settingsMaintenance, "GET", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tracks":1`) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	w = call(a.exportSettingsBackup, "POST", `{}`)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("backup: %d %s", w.Code, w.Body.String())
	}
	backup := append([]byte{}, w.Body.Bytes()...)
	if strings.Contains(string(backup), "synthetic-user") || strings.Contains(string(backup), "synthetic-password") {
		t.Fatal("backup leaks secrets")
	}
	w = call(a.cleanupSettingsLibrary, "POST", `{"kind":"catalog","confirm":"wrong"}`)
	if w.Code != 400 {
		t.Fatal("cleanup without confirm")
	}
	w = call(a.restoreSettingsBackup, "POST", `{"confirm":"wrong","backup":`+string(backup)+`}`)
	if w.Code != 400 {
		t.Fatal("restore without confirm")
	}
	w = call(a.cleanupSettingsLibrary, "POST", `{"kind":"catalog","confirm":"CLEAN"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":1`) {
		t.Fatalf("cleanup: %d %s", w.Code, w.Body.String())
	}
	body, err := json.Marshal(map[string]any{"confirm": "RESTORE", "backup": json.RawMessage(backup)})
	if err != nil {
		t.Fatal(err)
	}
	w = call(a.restoreSettingsBackup, "POST", string(body))
	if w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	w = call(a.restoreSettingsBackup, "POST", `{"confirm":"RESTORE","backup":{"schema_version":2}}`)
	if w.Code != 400 {
		t.Fatal("invalid backup accepted")
	}
	w = call(a.cleanupSettingsLibrary, "POST", `{"kind":"audit","before":0,"confirm":"CLEAN"}`)
	if w.Code != 400 {
		t.Fatal("invalid audit cutoff accepted")
	}
	w = call(a.cleanupSettingsLibrary, "POST", `{"kind":"/private/log","confirm":"CLEAN"}`)
	if w.Code != 400 || strings.Contains(w.Body.String(), "/private/log") {
		t.Fatal("path accepted or leaked")
	}
}

func TestSettingsMaintenanceBlocksPlaybackAndUnknownState(t *testing.T) {
	a, st, cookie := maintenanceTestAPI(t)
	if _, err := st.DB.Exec(`INSERT INTO nodes(node_id,playing,registered_at) VALUES('playing-node',1,1)`); err != nil {
		t.Fatal(err)
	}
	backup, err := a.lib.ExportLibrary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		h    http.HandlerFunc
		body string
	}{
		{a.cleanupSettingsLibrary, `{"kind":"catalog","confirm":"CLEAN"}`},
		{a.restoreSettingsBackup, `{"confirm":"RESTORE","backup":` + string(backup) + `}`},
	} {
		r := adminTestRequest("POST", test.body)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		test.h(w, r)
		if w.Code != 409 {
			t.Fatalf("playing-node operation: %d %s", w.Code, w.Body.String())
		}
	}
	a.nodes = nil
	r := adminTestRequest("POST", `{"kind":"catalog","confirm":"CLEAN"}`)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.cleanupSettingsLibrary(w, r)
	if w.Code != 503 {
		t.Fatal("unknown node state allowed")
	}
}
