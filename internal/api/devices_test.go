package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestNodeDeviceRegistry covers seeding, admin gating and rename validation.
func TestNodeDeviceRegistry(t *testing.T) {
	base := setup(t, "dev")
	nid := registerNode(t, base, "dev-reg")

	s, b := doJSON(t, "GET", base+"/nodes/"+nid+"/devices", nil)
	if s != 200 {
		t.Fatalf("list devices: %d %s", s, b)
	}
	devs := mustJSON(t, b)["devices"].([]any)
	if len(devs) != 1 || devs[0].(map[string]any)["hw"] != "default" || devs[0].(map[string]any)["enabled"] != true {
		t.Fatalf("expected seeded enabled default device: %s", b)
	}

	origin := strings.TrimSuffix(base, "/api/v1")
	fresh := &http.Client{}
	req, _ := http.NewRequest("POST", base+"/nodes/"+nid+"/devices/hw:2,0/activate", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	resp, err := fresh.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("activate without admin expected 401, got %d", resp.StatusCode)
	}

	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/devices/hw:2,0/activate", map[string]any{"enabled": true}); s != 200 {
		t.Fatalf("activate: %d %s", s, b)
	}
	if s, b := doJSON(t, "PATCH", base+"/nodes/"+nid+"/devices/hw:2,0", map[string]any{"name": "客厅 DAC"}); s != 200 {
		t.Fatalf("rename: %d %s", s, b)
	}
	s, b = doJSON(t, "GET", base+"/nodes/"+nid+"/devices", nil)
	devs = mustJSON(t, b)["devices"].([]any)
	var renamed map[string]any
	for _, d := range devs {
		m := d.(map[string]any)
		if m["hw"] == "hw:2,0" {
			renamed = m
		}
	}
	if renamed == nil || renamed["name"] != "客厅 DAC" || renamed["enabled"] != true || renamed["present"] != false {
		t.Fatalf("renamed device wrong: %s", b)
	}
	if s, b := doJSON(t, "PATCH", base+"/nodes/"+nid+"/devices/hw:2,0", map[string]any{"name": strings.Repeat("x", 41)}); s != 400 {
		t.Fatalf("overlong name expected 400, got %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/devices/hw:2,0/activate", map[string]any{"enabled": false}); s != 200 {
		t.Fatalf("deactivate: %d %s", s, b)
	}
	s, b = doJSON(t, "GET", base+"/nodes/"+nid+"/devices", nil)
	for _, d := range mustJSON(t, b)["devices"].([]any) {
		m := d.(map[string]any)
		if m["hw"] == "hw:2,0" && m["enabled"] != false {
			t.Fatalf("deactivate not persisted: %s", b)
		}
	}
}