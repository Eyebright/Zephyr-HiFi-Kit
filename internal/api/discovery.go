package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// discoverNode answers a terminal's auto-discovery probe. The reply is signed
// with that terminal's node secret, so only the real server can produce it and
// a rogue host on the LAN cannot redirect the agent.
func (a *API) discoverNode(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.DiscoveryEnabled || a.nodes == nil {
		writeErrorCode(w, http.StatusNotFound, "not_found", "发现服务未启用")
		return
	}
	nodeID := r.URL.Query().Get("node_id")
	nonce := r.URL.Query().Get("nonce")
	if nodeID == "" || nonce == "" || len(nodeID) > 128 || len(nonce) > 128 {
		writeErrorCode(w, http.StatusBadRequest, "bad_request", "node_id 与 nonce 不能为空")
		return
	}
	n, err := a.nodes.Get(nodeID)
	if err != nil || n.Secret == "" {
		// Do not reveal whether a node id exists.
		writeErrorCode(w, http.StatusNotFound, "not_found", "未找到可发现的终端")
		return
	}
	port := zatPort(a.cfg.ZATAddr)
	localIP := ""
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		localIP, _, _ = net.SplitHostPort(addr.String())
	}
	mac := hmac.New(sha256.New, []byte(n.Secret))
	fmt.Fprintf(mac, "%s|%s|%s|%s|%d", nodeID, nonce, a.cfg.DiscoveryID, localIP, port)
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": a.cfg.DiscoveryID,
		"zat_port":  port,
		"hmac":      hex.EncodeToString(mac.Sum(nil)),
	})
}

func zatPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 7800
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 {
		return 7800
	}
	return n
}
