// Package serverpush tells a terminal which management-server address to use.
// It first proves the terminal's identity with a node-secret HMAC, so the node
// secret is never sent to an address that DHCP may have reassigned.
package serverpush

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// ErrUnsupported marks agents that predate the endpoint-push API.
var ErrUnsupported = errors.New("terminal does not support server endpoint push")

type Result struct {
	Pushed    bool
	Changed   bool
	ServerURL string
}

// Push verifies the terminal at nodeIP:8080, then sends the advertised server
// address built from the local interface used to reach that terminal.
func Push(ctx context.Context, nodeID, nodeIP, secret string, httpPort, zatPort int) (Result, error) {
	if nodeID == "" || nodeIP == "" || secret == "" {
		return Result{}, fmt.Errorf("missing node identity")
	}
	if httpPort <= 0 {
		httpPort = 8080
	}
	if zatPort <= 0 {
		zatPort = 7800
	}
	agentAddr := net.JoinHostPort(nodeIP, "8080")
	return pushTo(ctx, nodeID, agentAddr, secret, httpPort, zatPort)
}

func pushTo(ctx context.Context, nodeID, agentAddr, secret string, httpPort, zatPort int) (Result, error) {
	// The local address of the route to the terminal is exactly the server IP
	// that terminal can reach.
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", agentAddr)
	if err != nil {
		return Result{}, err
	}
	localIP, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	conn.Close()
	if localIP == "" {
		return Result{}, fmt.Errorf("cannot determine server address")
	}

	nonceRaw := make([]byte, 16)
	if _, err := rand.Read(nonceRaw); err != nil {
		return Result{}, err
	}
	nonce := hex.EncodeToString(nonceRaw)
	client := &http.Client{
		Timeout:       6 * time.Second,
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s/local/api/identity?nonce=%s", agentAddr, nonce), nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Result{}, ErrUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("identity status %d", resp.StatusCode)
	}
	var id struct {
		NodeID string `json:"node_id"`
		HMAC   string `json:"hmac"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&id); err != nil {
		return Result{}, err
	}
	if id.NodeID != nodeID || !verify(secret, nodeID, nonce, id.HMAC) {
		return Result{}, fmt.Errorf("terminal identity mismatch")
	}

	serverURL := "http://" + net.JoinHostPort(localIP, strconv.Itoa(httpPort))
	body, _ := json.Marshal(map[string]string{
		"server_url": serverURL,
		"zat_addr":   net.JoinHostPort(localIP, strconv.Itoa(zatPort)),
	})
	req, err = http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+agentAddr+"/local/api/server-endpoint", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Secret", secret)
	resp, err = client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("endpoint update status %d", resp.StatusCode)
	}
	var out struct {
		OK      bool `json:"ok"`
		Changed bool `json:"changed"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
	return Result{Pushed: out.OK, Changed: out.Changed, ServerURL: serverURL}, nil
}

func verify(secret, nodeID, nonce, got string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|server-endpoint", nodeID, nonce)
	want, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	return hmac.Equal(mac.Sum(nil), want)
}
