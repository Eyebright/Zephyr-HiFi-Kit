package api

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"hifihub/server/internal/domain"
)

// hopHeaders are connection-scoped headers that must not be forwarded.
var hopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func (a *API) consoleProxy(w http.ResponseWriter, r *http.Request) {
	a.proxyToNode(w, r, false)
}

func (a *API) localAPIProxy(w http.ResponseWriter, r *http.Request) {
	a.proxyToNode(w, r, true)
}

// proxyToNode forwards to the agent's local HTTP service on port 8080. The
// console variant strips the proxy prefix; the local variant keeps /local/api/.
// The node secret travels only in the X-Node-Secret header and is never logged.
func (a *API) proxyToNode(w http.ResponseWriter, r *http.Request, keepPrefix bool) {
	n, err := a.nodes.Get(r.PathValue("id"))
	if err != nil || n.IP == "" || n.Status != domain.NodeOnline {
		writeErrorCode(w, http.StatusBadGateway, "node_console_unavailable", "终端不在线或不存在，无法连接终端控制台")
		return
	}
	target, err := url.Parse("http://" + net.JoinHostPort(n.IP, "8080"))
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "node_address_invalid", "终端地址无效")
		return
	}
	path := "/" + r.PathValue("rest")
	if keepPrefix {
		path = "/local/api/" + r.PathValue("rest")
	}
	secret := n.Secret
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = path
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.Out.Host = target.Host
			for _, h := range hopHeaders {
				pr.Out.Header.Del(h)
			}
			// Never trust a client-supplied secret; always use the stored one.
			pr.Out.Header.Set("X-Node-Secret", secret)
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ResponseHeaderTimeout: 10 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeErrorCode(w, http.StatusBadGateway, "node_console_connect_failed", "无法连接终端控制台服务")
		},
	}
	proxy.ServeHTTP(w, r)
}
