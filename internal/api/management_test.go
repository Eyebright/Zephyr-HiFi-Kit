package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedPublishedBOM creates an app, a (dev-mode) artifact and a published BOM,
// returning its version. Each test has its own store, so the app is unique.
func seedPublishedBOM(t *testing.T, base, bomID string) int {
	t.Helper()
	if s, b := doJSON(t, "POST", base+"/apps", map[string]any{"app_id": "testapp", "name": "Test", "category": "renderer"}); s != 201 {
		t.Fatalf("create app: %d %s", s, b)
	}
	if s, b := upload(t, base, "testapp", map[string]any{
		"version": "1.0.0", "arch": "arm64", "format": "deb", "units": []string{"testapp.service"},
	}, []byte("artifact"), nil); s != 201 {
		t.Fatalf("upload: %d %s", s, b)
	}
	s, b := doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": bomID, "role": "renderer",
		"apps": []map[string]any{{"app_id": "testapp", "version": "1.0.0"}},
	})
	if s != 201 {
		t.Fatalf("create bom: %d %s", s, b)
	}
	ver := int(mustJSON(t, b)["version"].(float64))
	if s, b := doJSON(t, "POST", base+"/boms/"+bomID+"/versions/1/publish?arch=arm64", nil); s != 200 {
		t.Fatalf("publish: %d %s", s, b)
	}
	return ver
}

// driveJobDone advances a job through the server gate and all agent phases.
func driveJobDone(t *testing.T, base, jid string) {
	t.Helper()
	if s, b := doJSON(t, "POST", base+"/jobs/"+jid+"/advance", nil); s != 200 {
		t.Fatalf("advance: %d %s", s, b)
	}
	for _, ph := range []string{"downloading", "verifying", "installing", "rendering_config", "enabling_units", "validating"} {
		if s, b := doJSON(t, "POST", base+"/jobs/"+jid+"/phases", map[string]any{"phase": ph, "state": "ok"}); s != 200 {
			t.Fatalf("phase %s: %d %s", ph, s, b)
		}
	}
}

func TestPatchNodeValidation(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "node-a")
	registerNode(t, base, "node-b")
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"empty alias", map[string]any{"alias": ""}, 400},
		{"alias too long", map[string]any{"alias": strings.Repeat("x", 41)}, 400},
		{"duplicate alias", map[string]any{"alias": "node-b"}, 400},
		{"group too long", map[string]any{"group": strings.Repeat("g", 25)}, 400},
		{"note too long", map[string]any{"note": strings.Repeat("n", 201)}, 400},
		{"bom missing", map[string]any{"bom_id": "nope"}, 400},
		{"valid rename", map[string]any{"alias": "renamed", "group": "living", "note": "hi"}, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s, b := doJSON(t, "PATCH", base+"/nodes/"+nid, c.body); s != c.want {
				t.Fatalf("got %d want %d (%s)", s, c.want, b)
			}
		})
	}
	// offline_after clamps to [30,3600]
	s, b := doJSON(t, "PATCH", base+"/nodes/"+nid, map[string]any{"offline_after": 5})
	if s != 200 || mustJSON(t, b)["offline_after"].(float64) != 30 {
		t.Fatalf("clamp low: %d %s", s, b)
	}
	s, b = doJSON(t, "PATCH", base+"/nodes/"+nid, map[string]any{"offline_after": 99999})
	if s != 200 || mustJSON(t, b)["offline_after"].(float64) != 3600 {
		t.Fatalf("clamp high: %d %s", s, b)
	}
}

func TestDLWhitelist(t *testing.T) {
	_, root, dir := setupFull(t, "dev")
	dist := filepath.Join(dir, "dist")
	os.WriteFile(filepath.Join(dist, "agent-install.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dist, "hifihub-agent_0.1.0_amd64.deb"), []byte("deb"), 0o644)
	os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte("x"), 0o644)
	cases := []struct {
		name string
		want int
	}{
		{"agent-install.sh", 200},
		{"hifihub-agent_0.1.0_amd64.deb", 200},
		{"SHA256SUMS", 200},
		{"evil.txt", 404},
		{"passwd", 404},
		{"hifihub-agent_0.1.0_mips.deb", 404},
		{"hifihub-agent_9.9.9_arm64.deb", 404}, // valid pattern, file absent
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Get(root + "/dl/" + c.name)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("got %d want %d", resp.StatusCode, c.want)
			}
		})
	}
}

func TestLicenseSeatGate(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "seat-a")
	if s, _ := doJSON(t, "POST", base+"/nodes/"+nid+"/authorize", nil); s != 409 {
		t.Fatalf("authorize without license: %d", s)
	}
	activateDevLicense(t, base, 1)
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/authorize", nil); s != 200 {
		t.Fatalf("authorize: %d %s", s, b)
	}
	nid2 := registerNode(t, base, "seat-b")
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid2+"/authorize", nil); s != 409 {
		t.Fatalf("seat full expected 409: %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/revoke", nil); s != 200 {
		t.Fatalf("revoke: %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid2+"/authorize", nil); s != 200 {
		t.Fatalf("authorize after revoke: %d %s", s, b)
	}
}

func TestCommandRoundtrip(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "cmd-node")
	if s, _ := doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{"type": "bogus"}); s != 400 {
		t.Fatalf("bad type: %d", s)
	}
	s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{"type": "os-check"})
	if s != 201 {
		t.Fatalf("create cmd: %d %s", s, b)
	}
	cid := mustJSON(t, b)["id"].(string)
	if _, b := doJSON(t, "GET", base+"/nodes/"+nid+"/commands?status=pending", nil); !bytes.Contains(b, []byte(cid)) {
		t.Fatalf("pending missing cmd: %s", b)
	}
	s, b = doJSON(t, "POST", base+"/commands/"+cid+"/result", map[string]any{
		"status": "ok", "log": "checked",
		"data": map[string]any{"reboot_required": true, "packages": []map[string]any{{"name": "foo", "current": "1.0", "candidate": "1.1"}}},
	})
	if s != 200 || mustJSON(t, b)["status"] != "done" {
		t.Fatalf("result: %d %s", s, b)
	}
	if s, _ := doJSON(t, "POST", base+"/commands/"+cid+"/result", map[string]any{"status": "ok"}); s != 409 {
		t.Fatalf("dup result: %d", s)
	}
	_, b = doJSON(t, "GET", base+"/nodes/"+nid+"/osupdates", nil)
	if mustJSON(t, b)["reboot_required"] != true {
		t.Fatalf("reboot_required: %s", b)
	}
	_, b = doJSON(t, "GET", base+"/nodes/"+nid+"/netdiag", nil)
	if res := mustJSON(t, b)["results"].([]any); len(res) != 0 {
		t.Fatalf("netdiag default: %s", b)
	}
}

func TestRolloutCreateAdvance(t *testing.T) {
	base := setup(t, "dev")
	ver := seedPublishedBOM(t, base, "ro-bom")
	nid := registerNode(t, base, "ro-node")
	activateDevLicense(t, base, 5)
	doJSON(t, "POST", base+"/nodes/"+nid+"/authorize", nil)
	doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{"playing": false})
	s, b := doJSON(t, "POST", base+"/rollouts", map[string]any{
		"name": "canary", "bom_id": "ro-bom", "bom_version": ver,
		"rings": []map[string]any{{"name": "r0", "percent": 50}, {"name": "r1", "percent": 50}},
	})
	if s != 201 {
		t.Fatalf("create rollout: %d %s", s, b)
	}
	ro := mustJSON(t, b)
	roid := ro["id"].(string)
	jobs := ro["rings"].([]any)[0].(map[string]any)["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("ring0 jobs: %v", jobs)
	}
	jid := jobs[0].(string)
	if s, _ := doJSON(t, "POST", base+"/rollouts/"+roid+"/advance", nil); s != 409 {
		t.Fatalf("busy advance: %d", s)
	}
	driveJobDone(t, base, jid)
	s, b = doJSON(t, "POST", base+"/rollouts/"+roid+"/advance", nil)
	if s != 200 || mustJSON(t, b)["current_ring"].(float64) != 1 {
		t.Fatalf("advance1: %d %s", s, b)
	}
	_, b = doJSON(t, "POST", base+"/rollouts/"+roid+"/advance", nil)
	if mustJSON(t, b)["status"] != "done" {
		t.Fatalf("rollout status: %s", b)
	}
	if s, _ := doJSON(t, "DELETE", base+"/rollouts/"+roid, nil); s != 200 {
		t.Fatalf("delete rollout: %d", s)
	}
}

func TestBomExportImportRollback(t *testing.T) {
	base := setup(t, "dev")
	ver := seedPublishedBOM(t, base, "exp-bom")
	s, b := doJSON(t, "GET", base+"/boms/exp-bom/versions/1/export", nil)
	if s != 200 {
		t.Fatalf("export: %d %s", s, b)
	}
	exported := mustJSON(t, b)
	s, b = doJSON(t, "POST", base+"/boms/import", exported)
	if s != 201 {
		t.Fatalf("import: %d %s", s, b)
	}
	imp := mustJSON(t, b)
	if imp["status"] != "draft" || int(imp["version"].(float64)) != ver+1 {
		t.Fatalf("import: %s", b)
	}
	s, b = doJSON(t, "POST", base+"/boms/exp-bom/rollback", map[string]any{"version": ver})
	if s != 201 {
		t.Fatalf("rollback: %d %s", s, b)
	}
	rb := mustJSON(t, b)
	if rb["status"] != "published" || int(rb["version"].(float64)) != ver+2 {
		t.Fatalf("rollback: %s", b)
	}
}

func TestAuditAndAlerts(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "audit-node")
	activateDevLicense(t, base, 5)
	doJSON(t, "POST", base+"/nodes/"+nid+"/authorize", nil)

	s, b := doJSON(t, "GET", base+"/audit", nil)
	if s != 200 {
		t.Fatalf("audit: %d %s", s, b)
	}
	var entries []map[string]any
	json.Unmarshal(b, &entries)
	if len(entries) == 0 {
		t.Fatal("audit empty")
	}
	_, b = doJSON(t, "GET", base+"/audit?limit=1", nil)
	json.Unmarshal(b, &entries)
	if len(entries) != 1 {
		t.Fatalf("limit: %d", len(entries))
	}
	resp, err := http.Get(base + "/audit/export?format=csv")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("csv export: %d %s", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}

	// reboot_required alert via an os-check command result
	_, b = doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{"type": "os-check"})
	cid := mustJSON(t, b)["id"].(string)
	doJSON(t, "POST", base+"/commands/"+cid+"/result", map[string]any{"status": "ok", "data": map[string]any{"reboot_required": true}})
	if s, b := doJSON(t, "POST", base+"/alerts/scan", nil); s != 200 {
		t.Fatalf("scan: %d %s", s, b)
	}
	_, b = doJSON(t, "GET", base+"/alerts", nil)
	if !bytes.Contains(b, []byte("reboot_required")) {
		t.Fatalf("reboot alert missing: %s", b)
	}
	if s, b := doJSON(t, "POST", base+"/alerts/ack-all", nil); s != 200 {
		t.Fatalf("ack-all: %d %s", s, b)
	}
}

func TestMetricsAndProxyGuard(t *testing.T) {
	base, root, _ := setupFull(t, "dev")
	registerNode(t, base, "proxy-node")
	resp, err := http.Get(root + "/metrics/server")
	if err != nil {
		t.Fatal(err)
	}
	mb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metrics: %d", resp.StatusCode)
	}
	m := mustJSON(t, mb)
	if m["signature_mode"] != "dev" {
		t.Fatalf("metrics mode: %v", m["signature_mode"])
	}
	if _, ok := m["uptime_s"]; !ok {
		t.Fatal("metrics uptime missing")
	}
	// proxy to a missing node is a deterministic 502 JSON guard
	if s, _ := doJSON(t, "GET", base+"/nodes/does-not-exist/console/status", nil); s != 502 {
		t.Fatalf("proxy missing node: %d", s)
	}
	if s, _ := doJSON(t, "GET", base+"/nodes/does-not-exist/local/api/status", nil); s != 502 {
		t.Fatalf("local proxy missing node: %d", s)
	}
}

func TestNodeDeleteRestore(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "del-node")
	doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{"playing": true})
	if s, b := doJSON(t, "DELETE", base+"/nodes/"+nid, nil); s != 409 {
		t.Fatalf("delete playing: %d %s", s, b)
	}
	doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{"playing": false})
	_, b := doJSON(t, "GET", base+"/nodes/"+nid, nil)
	nodeJSON := mustJSON(t, b)
	if _, ok := nodeJSON["secret"]; ok {
		t.Fatal("secret leaked in node JSON")
	}
	s, b := doJSON(t, "DELETE", base+"/nodes/"+nid, nil)
	if s != 200 || mustJSON(t, b)["ok"] != true {
		t.Fatalf("delete: %d %s", s, b)
	}
	if s, _ := doJSON(t, "GET", base+"/nodes/"+nid, nil); s != 404 {
		t.Fatalf("get deleted: %d", s)
	}
	s, b = doJSON(t, "POST", base+"/nodes/restore", map[string]any{"node": nodeJSON})
	if s != 200 {
		t.Fatalf("restore: %d %s", s, b)
	}
	if s, _ := doJSON(t, "GET", base+"/nodes/"+nid, nil); s != 200 {
		t.Fatalf("get restored: %d", s)
	}
}
