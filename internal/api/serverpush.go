package api

import (
	"net"
	"net/http"
	"strconv"

	"hifihub/server/internal/serverpush"
)

// pushServerEndpoint sends this server's reachable address to a terminal's
// local console after proving the terminal's identity with its node secret.
func (a *API) pushServerEndpoint(w http.ResponseWriter, r *http.Request) {
	if a.nodes == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "not_found", "节点服务未启用")
		return
	}
	n, err := a.nodes.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if n.IP == "" {
		writeErrorCode(w, http.StatusBadRequest, "bad_request", "终端尚未上报 IP 地址")
		return
	}
	res, err := serverpush.Push(r.Context(), n.NodeID, n.IP, n.Secret, httpPort(a.cfg.ListenAddr), zatPort(a.cfg.ZATAddr))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": res.Pushed, "changed": res.Changed, "server_url": res.ServerURL,
	})
}

func httpPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 8080
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 {
		return 8080
	}
	return n
}
