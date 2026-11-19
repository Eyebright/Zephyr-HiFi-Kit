package api

import (
	"encoding/json"
	"testing"
)

func listOf(t *testing.T, b []byte) []any {
	t.Helper()
	var xs []any
	if err := json.Unmarshal(b, &xs); err != nil {
		t.Fatalf("not a JSON array: %s", b)
	}
	return xs
}

func registerNodeDistro(t *testing.T, base, alias, distro string) string {
	t.Helper()
	s, b := doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800})
	if s != 201 {
		t.Fatalf("issue token: %d %s", s, b)
	}
	tok := mustJSON(t, b)["token"].(string)
	s, b = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": tok, "alias": alias, "arch": "arm64", "distro": distro, "machine_id": "mid-" + alias,
	})
	if s != 201 {
		t.Fatalf("register %s: %d %s", alias, s, b)
	}
	return mustJSON(t, b)["node_id"].(string)
}

func TestAptSourceCRUDAndApply(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "POST", base+"/apt-sources", map[string]any{
		"name": "upmpdcli", "url": "http://repo.example/debian/", "key_url": "https://repo.example/key.gpg",
		"distros": []string{"bookworm"},
	})
	if s != 201 {
		t.Fatalf("create source: %d %s", s, b)
	}
	sid := mustJSON(t, b)["id"].(string)
	if s, b := doJSON(t, "POST", base+"/apt-sources", map[string]any{"name": "bad", "url": "ftp://x"}); s != 400 {
		t.Fatalf("bad url accepted: %d %s", s, b)
	}
	_, b = doJSON(t, "GET", base+"/apt-sources", nil)
	if got := len(listOf(t, b)); got != 1 {
		t.Fatalf("list sources: %d %s", got, b)
	}

	nBook := registerNodeDistro(t, base, "apt-book", "Debian GNU/Linux 12 (bookworm)")
	s, b = doJSON(t, "POST", base+"/nodes/"+nBook+"/apt-sources/apply", nil)
	if s != 201 {
		t.Fatalf("apply bookworm: %d %s", s, b)
	}
	cmd := mustJSON(t, b)
	if cmd["type"] != "apt-source" {
		t.Fatalf("command type: %s", b)
	}
	srcs := cmd["params"].(map[string]any)["sources"].([]any)
	if len(srcs) != 1 || srcs[0].(map[string]any)["id"] != sid {
		t.Fatalf("apply payload: %s", b)
	}

	nTrixie := registerNodeDistro(t, base, "apt-trixie", "Debian GNU/Linux 13 (trixie)")
	if s, _ := doJSON(t, "POST", base+"/nodes/"+nTrixie+"/apt-sources/apply", nil); s != 400 {
		t.Fatalf("trixie should have no matching source: %d", s)
	}
	if s, _ := doJSON(t, "POST", base+"/nodes/nope/apt-sources/apply", nil); s != 404 {
		t.Fatalf("unknown node: %d", s)
	}
	if s, _ := doJSON(t, "DELETE", base+"/apt-sources/"+sid, nil); s != 200 {
		t.Fatalf("delete source: %d", s)
	}
	_, b = doJSON(t, "GET", base+"/apt-sources", nil)
	if got := len(listOf(t, b)); got != 0 {
		t.Fatalf("list after delete: %d %s", got, b)
	}
}

func TestAptInstallPlayGate(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "apt-gate")
	s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{
		"type": "apt-install", "params": map[string]any{"packages": []string{"mpd"}},
	})
	if s != 201 {
		t.Fatalf("apt-install idle: %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{
		"agent_version": "0.4.1", "playing": true,
	}); s != 200 {
		t.Fatalf("heartbeat: %d %s", s, b)
	}
	if s, _ := doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{
		"type": "apt-install", "params": map[string]any{"packages": []string{"mpd"}},
	}); s != 409 {
		t.Fatalf("apt-install while playing should 409: %d", s)
	}
	// Configuring sources does not touch playback, so it stays allowed.
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/commands", map[string]any{
		"type": "apt-source", "params": map[string]any{"sources": []map[string]any{{"id": "x", "name": "x", "url": "http://r/"}}},
	}); s != 201 {
		t.Fatalf("apt-source while playing: %d %s", s, b)
	}
}

func TestAptAppsAndBomValidate(t *testing.T) {
	base := setup(t, "dev")
	if s, b := doJSON(t, "POST", base+"/apps", map[string]any{
		"app_id": "mpd", "name": "MPD", "category": "renderer",
	}); s != 201 {
		t.Fatalf("create app: %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/apps/mpd/versions", map[string]any{
		"version": "apt", "arch": "all", "format": "apt", "apt_package": "mpd",
	}); s != 201 {
		t.Fatalf("add apt version: %d %s", s, b)
	}
	_, b := doJSON(t, "GET", base+"/apt-apps", nil)
	apps := listOf(t, b)
	if len(apps) != 1 {
		t.Fatalf("apt-apps: %s", b)
	}
	entry := apps[0].(map[string]any)
	if entry["version"].(map[string]any)["apt_package"] != "mpd" {
		t.Fatalf("apt package: %s", b)
	}
	// An apt entry needs no signed artifact: BOM validation must accept it on
	// any node arch via the architecture-independent "all" version.
	if s, b := doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": "apt-bom", "role": "renderer",
		"apps": []map[string]any{{"app_id": "mpd", "version": "apt"}},
	}); s != 201 {
		t.Fatalf("create bom: %d %s", s, b)
	}
	s, b := doJSON(t, "POST", base+"/boms/apt-bom/versions/1/validate?arch=arm64", nil)
	if s != 200 {
		t.Fatalf("validate: %d %s", s, b)
	}
	rep := mustJSON(t, b)
	if errs, ok := rep["errors"].([]any); ok && len(errs) != 0 {
		t.Fatalf("apt bom should validate clean: %s", b)
	}
}
