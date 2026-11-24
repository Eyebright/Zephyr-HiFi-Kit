package api

import "testing"

func TestAndroidNumericInstallToken(t *testing.T) {
	base := setup(t, "off")
	s, b := doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800, "arch": "android"})
	if s != 201 {
		t.Fatalf("issue android token: %d %s", s, b)
	}
	tok := mustJSON(t, b)["token"].(string)
	if len(tok) != 8 {
		t.Fatalf("android token length = %d, want 8", len(tok))
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			t.Fatalf("android token not numeric: %q", tok)
		}
	}
	s, b = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": tok, "alias": "phone", "arch": "android", "distro": "android", "machine_id": "mid-phone",
	})
	if s != 201 {
		t.Fatalf("register android numeric: %d %s", s, b)
	}
	// A hex token must not register an Android node.
	s, b = doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800})
	if s != 201 {
		t.Fatalf("issue hex token: %d %s", s, b)
	}
	hexTok := mustJSON(t, b)["token"].(string)
	s, _ = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": hexTok, "alias": "phone2", "arch": "android", "distro": "android", "machine_id": "mid-phone2",
	})
	if s != 401 {
		t.Fatalf("android hex register status = %d, want 401", s)
	}
}