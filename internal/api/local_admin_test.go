package api

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestLocalTrustedRemoteRequiresAdmin(t *testing.T) {
	base, _, _, a := setupFullAPI(t, "dev")
	// Remote (non-loopback) admin mutation without a session must be rejected.
	if code := remoteGateCall(t, a, "POST", "/nodes/any/devices/hw:1,0/activate", `{"enabled":true}`); code != http.StatusUnauthorized {
		t.Fatalf("remote activate without session = %d, want 401", code)
	}
	if code := remoteGateCall(t, a, "PUT", "/nodes/any/devices/hw:1,0/dsp", `{"enabled":true}`); code != http.StatusUnauthorized {
		t.Fatalf("remote DSP update without session = %d, want 401", code)
	}
	origin := strings.TrimSuffix(base, "/api/v1")
	// Loopback without a session: the session endpoint auto-issues a trusted
	// session (no password prompt), reporting local+authenticated.
	jar, _ := cookiejar.New(nil)
	local := &http.Client{Jar: jar}
	req, _ := http.NewRequest("GET", base+"/settings/session", nil)
	req.Header.Set("Origin", origin)
	resp, err := local.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	m := mustJSON(t, body[:n])
	if resp.StatusCode != 200 || m["local"] != true || m["authenticated"] != true {
		t.Fatalf("loopback session should auto-authenticate: %d %s", resp.StatusCode, body[:n])
	}
	// With the auto-issued session, local admin mutations succeed.
	req, _ = http.NewRequest("POST", base+"/nodes/any/devices/hw:1,0/activate", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	resp, err = local.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("local activate with auto session should succeed, got 401")
	}
	// A loopback client that never obtained a session is still gated.
	bare := &http.Client{}
	req, _ = http.NewRequest("POST", base+"/nodes/any/devices/hw:1,0/activate", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	resp, err = bare.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("loopback activate without session = %d, want 401", resp.StatusCode)
	}
}