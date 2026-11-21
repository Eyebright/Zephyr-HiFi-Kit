package serverpush

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func sign(secret, nodeID, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|server-endpoint", nodeID, nonce)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestPushVerifiesIdentityBeforeSendingSecret(t *testing.T) {
	const secret = "node-secret"
	var updates int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/local/api/identity":
			nonce := r.URL.Query().Get("nonce")
			fmt.Fprintf(w, `{"node_id":"node-1","hmac":%q}`, sign(secret, "node-1", nonce))
		case "/local/api/server-endpoint":
			if r.Header.Get("X-Node-Secret") != secret {
				t.Errorf("missing secret")
			}
			atomic.AddInt32(&updates, 1)
			fmt.Fprint(w, `{"ok":true,"changed":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res, err := pushTo(context.Background(), "node-1", strings.TrimPrefix(srv.URL, "http://"), secret, 8080, 7800)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.Pushed || updates != 1 {
		t.Fatalf("result = %+v updates=%d", res, updates)
	}
	if !strings.Contains(res.ServerURL, "127.0.0.1") {
		t.Fatalf("server url = %s", res.ServerURL)
	}
}

func TestPushRejectsWrongIdentity(t *testing.T) {
	var updates int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/local/api/identity" {
			fmt.Fprint(w, `{"node_id":"node-1","hmac":"00"}`)
			return
		}
		atomic.AddInt32(&updates, 1)
	}))
	defer srv.Close()
	_, err := pushTo(context.Background(), "node-1", strings.TrimPrefix(srv.URL, "http://"), "node-secret", 8080, 7800)
	if err == nil {
		t.Fatal("expected identity mismatch")
	}
	if updates != 0 {
		t.Fatal("secret endpoint was called before identity verification")
	}
}
