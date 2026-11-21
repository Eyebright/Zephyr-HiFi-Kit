package ui

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

// Server hosts the local console and its REST API.
type Server struct {
	backend Backend
	mux     *http.ServeMux
}

// New builds the local UI server over a Backend.
func New(b Backend) *Server {
	s := &Server{backend: b, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root HTTP handler. No X-Frame-Options is set so the
// server can embed the console in its reverse-proxied node view.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	s.mux.Handle("GET /", files)

	// GET routes stay open (read-only).
	s.mux.HandleFunc("GET /local/api/status", s.status)
	s.mux.HandleFunc("GET /local/api/identity", s.identity)
	s.mux.HandleFunc("POST /local/api/server-endpoint", s.serverEndpoint)
	s.mux.HandleFunc("GET /local/api/services", s.services)
	s.mux.HandleFunc("GET /local/api/metrics", s.metrics)
	s.mux.HandleFunc("GET /local/api/catalog", s.catalog)
	// Loopback-only secret bootstrap for the standalone local console.
	s.mux.HandleFunc("GET /local/api/secret", s.secret)

	// Mutation routes are gated (X-Node-Secret, ALLOW_LOCAL, play-protection).
	s.mux.HandleFunc("POST /local/api/services/{app}/install", s.install)
	s.mux.HandleFunc("POST /local/api/services/{app}/uninstall", s.uninstall)
	s.mux.HandleFunc("POST /local/api/services/{app}/enable", s.enable)
	s.mux.HandleFunc("POST /local/api/services/{app}/disable", s.disable)
	s.mux.HandleFunc("POST /local/api/services/{app}/config", s.configure)
	s.mux.HandleFunc("POST /local/api/power", s.power)
	s.mux.HandleFunc("POST /local/api/os/check", s.osCheck)
	s.mux.HandleFunc("POST /local/api/os/upgrade", s.osUpgrade)
	s.mux.HandleFunc("POST /local/api/netdiag", s.netdiag)

	// ZAT playback: read is open; controls are gated by ALLOW_LOCAL + secret but
	// NOT play-protection (you must be able to stop/pause while playing).
	s.mux.HandleFunc("GET /local/api/playback", s.playback)
	s.mux.HandleFunc("GET /local/api/playback/queue", s.playbackQueue)
	s.mux.HandleFunc("POST /local/api/playback/pause", s.playbackPause)
	s.mux.HandleFunc("POST /local/api/playback/stop", s.playbackStop)
	s.mux.HandleFunc("POST /local/api/playback/volume", s.playbackVolume)
	s.mux.HandleFunc("POST /local/api/playback/seek", s.playbackSeek)
	s.mux.HandleFunc("POST /local/api/playback/device", s.playbackDevice)
	s.mux.HandleFunc("POST /local/api/playback/next", s.playbackNext)
	s.mux.HandleFunc("POST /local/api/playback/prev", s.playbackPrev)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func ctx(r *http.Request) context.Context {
	return r.Context()
}

// gate enforces the local mutation gates. It returns true when the action may
// proceed; otherwise it writes a 403 with a Chinese message and returns false.
// Order: ALLOW_LOCAL config gate, then X-Node-Secret, then play-protection.
// GET routes are never gated; power is additionally gated by ALLOW_POWER in the
// backend. An empty stored secret (pre-registration) skips the secret check.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) bool {
	if !s.gateBase(w, r) {
		return false
	}
	g := s.backend.GateState()
	if g.Playing && g.PlayProtect {
		writeErr(w, http.StatusForbidden, "FR-3.10：终端正在播放，请先停止播放再执行此操作。")
		return false
	}
	return true
}

// gateBase enforces ALLOW_LOCAL and the node secret, without play-protection.
// Playback controls use this so stop/pause remain available during playback.
// The standalone local console marks its mutations with X-Local-UI: browsers
// cannot set that header cross-origin without CORS approval, so it blocks CSRF
// while letting LAN browsers operate the terminal page directly. The server
// proxy authenticates with the stored node secret instead.
func (s *Server) gateBase(w http.ResponseWriter, r *http.Request) bool {
	g := s.backend.GateState()
	if !g.AllowLocal {
		writeErr(w, http.StatusForbidden, "本地控制已禁用（ALLOW_LOCAL=false），请通过服务端控制台操作。")
		return false
	}
	if g.Secret != "" && r.Header.Get("X-Node-Secret") != g.Secret && r.Header.Get("X-Local-UI") != "1" {
		writeErr(w, http.StatusForbidden, "缺少或错误的节点密钥（X-Node-Secret），无法执行本地操作。")
		return false
	}
	return true
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// identity answers a nonce challenge for server-initiated address pushes. The
// server verifies the HMAC before sending its node secret, so a DHCP-reassigned
// address cannot collect that secret.
func (s *Server) identity(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	if nonce == "" || len(nonce) > 128 {
		writeErr(w, http.StatusBadRequest, "nonce required")
		return
	}
	id := s.backend.Identity(ctx(r)).NodeID
	if id == "" {
		writeErr(w, http.StatusServiceUnavailable, "terminal is not registered yet")
		return
	}
	secret := s.backend.Secret()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|server-endpoint", id, nonce)
	writeJSON(w, http.StatusOK, map[string]any{"node_id": id, "hmac": hex.EncodeToString(mac.Sum(nil))})
}

func (s *Server) serverEndpoint(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	var b struct {
		ServerURL string `json:"server_url"`
		ZATAddr   string `json:"zat_addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON")
		return
	}
	changed, err := s.backend.SetServerEndpoint(b.ServerURL, b.ZATAddr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": changed})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	c := ctx(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"identity": s.backend.Identity(c),
		"metrics":  s.backend.Metrics(),
		"services": s.backend.Services(c),
		"dac":      s.backend.DAC(),
		"devices":  s.backend.Devices(),
	})
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.backend.Services(ctx(r)))
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.backend.Metrics())
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.backend.Catalog(ctx(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

type installBody struct {
	Version string            `json:"version"`
	Config  map[string]string `json:"config"`
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	var b installBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := s.backend.Install(ctx(r), r.PathValue("app"), b.Version, b.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) uninstall(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	if err := s.backend.Uninstall(ctx(r), r.PathValue("app")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) enable(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	if err := s.backend.Enable(r.PathValue("app")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) disable(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	if err := s.backend.Disable(r.PathValue("app")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) configure(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	var b struct {
		Config map[string]string `json:"config"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := s.backend.Configure(r.PathValue("app"), b.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) power(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	var b struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if b.Action != "reboot" && b.Action != "shutdown" {
		writeErr(w, http.StatusBadRequest, "action must be reboot or shutdown")
		return
	}
	if err := s.backend.Power(b.Action); err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": b.Action})
}

func (s *Server) osCheck(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.backend.OSCheck(ctx(r)))
}

func (s *Server) osUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	var b struct {
		Packages []string `json:"packages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	writeJSON(w, http.StatusOK, s.backend.OSUpgrade(ctx(r), b.Packages))
}

func (s *Server) netdiag(w http.ResponseWriter, r *http.Request) {
	if !s.gate(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.backend.Netdiag(ctx(r)))
}

func (s *Server) secret(w http.ResponseWriter, r *http.Request) {
	// Loopback-only: the standalone console served from the terminal itself may
	// read the secret to authenticate mutations. Remote/proxied callers rely on
	// the server injecting X-Node-Secret instead. The secret is never logged.
	if !isLoopback(r.RemoteAddr) {
		writeErr(w, http.StatusForbidden, "节点密钥仅对本机回环访问开放。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": s.backend.Secret()})
}

func (s *Server) playback(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.backend.Playback())
}

func (s *Server) playbackQueue(w http.ResponseWriter, r *http.Request) {
	q, err := s.backend.PlaybackQueue(ctx(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) playbackNext(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	if err := s.backend.PlaybackNext(ctx(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) playbackPrev(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	if err := s.backend.PlaybackPrev(ctx(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) playbackPause(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	paused, err := s.backend.PlaybackPause()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": paused})
}

func (s *Server) playbackStop(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	if err := s.backend.PlaybackStop(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) playbackVolume(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	var b struct {
		Volume int `json:"volume"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := s.backend.PlaybackVolume(b.Volume); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "volume": b.Volume})
}

func (s *Server) playbackSeek(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	var b struct {
		PTS uint64 `json:"pts"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := s.backend.PlaybackSeek(b.PTS); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pts": b.PTS})
}

func (s *Server) playbackDevice(w http.ResponseWriter, r *http.Request) {
	if !s.gateBase(w, r) {
		return
	}
	var b struct {
		Device string `json:"device"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := s.backend.PlaybackDevice(b.Device); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": b.Device})
}
