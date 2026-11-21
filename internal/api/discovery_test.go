package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
)

func TestDiscoverEndpointSignsNodeSecret(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800})
	if s != 201 {
		t.Fatalf("issue token: %d %s", s, b)
	}
	tok := mustJSON(t, b)["token"].(string)
	s, b = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": tok, "alias": "disc", "arch": "arm64", "distro": "debian13", "machine_id": "mid-disc",
	})
	if s != 201 {
		t.Fatalf("register: %d %s", s, b)
	}
	reg := mustJSON(t, b)
	nodeID, secret := reg["node_id"].(string), reg["secret"].(string)

	s, b = doJSON(t, "GET", base+"/discover?node_id="+nodeID+"&nonce=abc123", nil)
	if s != 200 {
		t.Fatalf("discover: %d %s", s, b)
	}
	reply := mustJSON(t, b)
	if reply["server_id"] != "test-server" || reply["zat_port"].(float64) != 7800 {
		t.Fatalf("reply = %v", reply)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|%s|%s|%d", nodeID, "abc123", "test-server", "127.0.0.1", 7800)
	if reply["hmac"] != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("hmac mismatch: %v", reply)
	}

	s, _ = doJSON(t, "GET", base+"/discover?node_id=n-missing&nonce=abc", nil)
	if s != http.StatusNotFound {
		t.Fatalf("unknown node status = %d", s)
	}
}
