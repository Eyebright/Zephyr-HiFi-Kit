package discover

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func signedReply(nodeID, nonce, secret, serverID string, zatPort int) reply {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|%s|%s|%d", nodeID, nonce, serverID, "192.168.0.41", zatPort)
	return reply{ServerID: serverID, ZATPort: zatPort, HMAC: hex.EncodeToString(mac.Sum(nil))}
}

func TestVerifyDiscoveryReply(t *testing.T) {
	r := signedReply("node-1", "abc", "s3cret", "srv-9", 7800)
	if !verify("node-1", "abc", "192.168.0.41", r, "s3cret") {
		t.Fatal("valid signature rejected")
	}
	if verify("node-1", "abc", "192.168.0.41", r, "wrong") {
		t.Fatal("wrong secret accepted")
	}
	bad := r
	bad.ZATPort = 7801
	if verify("node-1", "abc", "192.168.0.41", bad, "s3cret") {
		t.Fatal("tampered port accepted")
	}
}
