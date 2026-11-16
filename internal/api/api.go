// Package api exposes the REST surface (FR-9.2). Realtime WS and mTLS are
// layered on in a later increment; this file wires the JSON HTTP handlers.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/alerts"
	"hifihub/server/internal/audit"
	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/config"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/keys"
	"hifihub/server/internal/library"
	"hifihub/server/internal/license"
	"hifihub/server/internal/metrics"
	"hifihub/server/internal/node"
	"hifihub/server/internal/repo"
	"hifihub/server/internal/rollouts"
	"hifihub/server/internal/task"
	"hifihub/server/internal/update"
	"hifihub/server/internal/zatserver"
)

// Deps aggregates the services backing the API.
type Deps struct {
	Cat      *catalog.Service
	Repo     *repo.Service
	Boms     *bom.Service
	Nodes    *node.Service
	Tasks    *task.Service
	License  *license.Service
	Keys     *keys.Service
	Audit    *audit.Service
	Alerts   *alerts.Service
	Commands *commands.Service
	Rollouts *rollouts.Service
	Metrics  *metrics.Collector
	Library  *library.Service
	ZAT      *zatserver.Server
	Update   *update.Service
}

// API aggregates the services and the router.
type API struct {
	cfg    config.Config
	cat    *catalog.Service
	repo   *repo.Service
	boms   *bom.Service
	nodes  *node.Service
	tasks  *task.Service
	lic    *license.Service
	keys   *keys.Service
	aud    *audit.Service
	alerts *alerts.Service
	cmds   *commands.Service
	rolls  *rollouts.Service
	met    *metrics.Collector
	lib    *library.Service
	zat    *zatserver.Server
	upd    *update.Service
	mux    *http.ServeMux
}

// New builds the API and registers routes.
func New(cfg config.Config, d Deps) *API {
	a := &API{
		cfg: cfg, cat: d.Cat, repo: d.Repo, boms: d.Boms, nodes: d.Nodes, tasks: d.Tasks,
		lic: d.License, keys: d.Keys, aud: d.Audit, alerts: d.Alerts, cmds: d.Commands,
		rolls: d.Rollouts, met: d.Metrics, lib: d.Library, zat: d.ZAT, upd: d.Update, mux: http.NewServeMux(),
	}
	a.routes()
	return a
}

// Handler returns the root HTTP handler.
func (a *API) Handler() http.Handler { return a.mux }

// route registers a handler; every mutation is wrapped with the audit log.
func (a *API) route(pattern string, h http.HandlerFunc) {
	a.mux.HandleFunc(pattern, a.withAudit(h))
}

func (a *API) routes() {
	a.route("GET /api/v1/health", a.health)

	// Catalog (FR-3.1)
	a.route("GET /api/v1/apps", a.listApps)
	a.route("POST /api/v1/apps", a.createApp)
	a.route("GET /api/v1/apps/{id}", a.getApp)
	a.route("GET /api/v1/apps/{id}/versions", a.listVersions)
	a.route("POST /api/v1/apps/{id}/versions", a.addVersion)
	a.route("POST /api/v1/apps/{id}/versions/{ver}/{arch}/ban", a.banVersion)
	a.route("POST /api/v1/apps/{id}/versions/{ver}/{arch}/lifecycle", a.lifecycleVersion)

	// Artifact repository (FR-3.3)
	a.route("GET /api/v1/repo/{app}/{ver}/{arch}", a.downloadArtifact)
	a.route("GET /api/v1/repo/{app}/{ver}/{arch}/sig", a.downloadSignature)

	// BOM (FR-3.2)
	a.route("GET /api/v1/boms", a.listBoms)
	a.route("POST /api/v1/boms", a.createBom)
	a.route("POST /api/v1/boms/import", a.importBom)
	a.route("GET /api/v1/boms/{id}", a.latestBom)
	a.route("GET /api/v1/boms/{id}/versions", a.listBomVersions)
	a.route("GET /api/v1/boms/{id}/versions/{ver}", a.getBom)
	a.route("GET /api/v1/boms/{id}/versions/{ver}/export", a.exportBom)
	a.route("POST /api/v1/boms/{id}/versions/{ver}/validate", a.validateBom)
	a.route("POST /api/v1/boms/{id}/versions/{ver}/publish", a.publishBom)
	a.route("POST /api/v1/boms/{id}/rollback", a.rollbackBom)

	// Nodes (FR-4)
	a.route("POST /api/v1/nodes/token", a.issueToken)
	a.route("GET /api/v1/nodes/token/{token}", a.tokenStatus)
	a.route("POST /api/v1/nodes/register", a.registerNode)
	a.route("POST /api/v1/nodes/restore", a.restoreNode)
	a.route("GET /api/v1/nodes", a.listNodes)
	a.route("GET /api/v1/nodes/{id}", a.getNode)
	a.route("PATCH /api/v1/nodes/{id}", a.patchNode)
	a.route("DELETE /api/v1/nodes/{id}", a.deleteNode)
	a.route("POST /api/v1/nodes/{id}/authorize", a.authorizeNode)
	a.route("POST /api/v1/nodes/{id}/revoke", a.revokeNode)
	a.route("POST /api/v1/nodes/{id}/heartbeat", a.heartbeat)
	a.route("POST /api/v1/nodes/{id}/snapshot", a.snapshot)
	a.route("POST /api/v1/nodes/{id}/commands", a.createCommand)
	// GET node sub-resources are dispatched through one single-segment wildcard
	// instead of separate literal routes: a literal "/nodes/token/{token}" would
	// otherwise ambiguity-conflict with "/nodes/{id}/apps" (and friends) because
	// the literal and wildcard cross at different positions. The token route is
	// strictly more specific, so it still wins for /nodes/token/{token}.
	a.route("GET /api/v1/nodes/{id}/{sub}", a.nodeSubresource)
	a.route("POST /api/v1/commands/{id}/result", a.commandResult)

	// Agent console proxies (any method; GET is not audited).
	a.route("/api/v1/nodes/{id}/console/{rest...}", a.consoleProxy)
	a.route("/api/v1/nodes/{id}/local/api/{rest...}", a.localAPIProxy)

	// Jobs (FR-3.5 / FR-3.10)
	a.route("POST /api/v1/jobs", a.createJob)
	a.route("GET /api/v1/jobs", a.listJobs)
	a.route("GET /api/v1/jobs/{id}", a.getJob)
	a.route("POST /api/v1/jobs/{id}/advance", a.advanceJob)
	a.route("POST /api/v1/jobs/{id}/phases", a.reportPhase)
	a.route("POST /api/v1/jobs/{id}/rollback-complete", a.rollbackComplete)

	// License (FR-6)
	a.route("GET /api/v1/license", a.getLicense)
	a.route("GET /api/v1/license/receipt", a.getLicenseReceipt)
	a.route("POST /api/v1/license/activate", a.activateLicense)

	// Audit (FR-7.2)
	a.route("GET /api/v1/audit", a.listAudit)
	a.route("GET /api/v1/audit/export", a.exportAudit)

	// Alerts (FR-7.1)
	a.route("GET /api/v1/alerts", a.listAlerts)
	a.route("POST /api/v1/alerts/{id}/ack", a.ackAlert)
	a.route("POST /api/v1/alerts/{id}/resolve", a.resolveAlert)
	a.route("POST /api/v1/alerts/ack-all", a.ackAllAlerts)
	a.route("POST /api/v1/alerts/scan", a.scanAlerts)

	// GPG keys / trust root (NFR-S2)
	a.route("GET /api/v1/keys", a.listKeys)
	a.route("POST /api/v1/keys/import", a.importKey)
	a.route("POST /api/v1/keys/{id}/trust-root", a.setTrustRoot)
	a.route("GET /api/v1/keys/trust-root.asc", a.exportTrustRoot)

	// Staged rollouts (FR-3.5)
	a.route("GET /api/v1/rollouts", a.listRollouts)
	a.route("POST /api/v1/rollouts", a.createRollout)
	a.route("GET /api/v1/rollouts/{id}", a.getRollout)
	a.route("POST /api/v1/rollouts/{id}/advance", a.advanceRollout)
	a.route("DELETE /api/v1/rollouts/{id}", a.deleteRollout)

	// Version convergence: server self-update + fleet auto-iteration.
	a.route("GET /api/v1/update/status", a.updateStatus)
	a.route("POST /api/v1/update/check", a.updateCheck)
	a.route("POST /api/v1/update/apply", a.updateApply)
	a.route("POST /api/v1/update/auto", a.updateAuto)
	a.route("GET /api/v1/update/fleet", a.updateFleet)
	a.route("POST /api/v1/update/fleet/policy", a.updateFleetPolicy)
	a.route("POST /api/v1/update/fleet/converge", a.updateFleetConverge)
	a.route("POST /api/v1/update/fleet/{id}/retry", a.updateFleetRetry)

	// Media library: SMB mounts + scanned tracks (Zephyr Music Player).
	a.route("GET /api/v1/mounts", a.listMounts)
	a.route("POST /api/v1/mounts", a.addMount)
	a.route("GET /api/v1/mounts/{id}", a.getMount)
	a.route("DELETE /api/v1/mounts/{id}", a.deleteMount)
	a.route("POST /api/v1/mounts/{id}/mount", a.mountShare)
	a.route("POST /api/v1/mounts/{id}/unmount", a.unmountShare)
	a.route("POST /api/v1/mounts/{id}/scan", a.scanMount)
	a.route("GET /api/v1/tracks", a.listTracks)
	a.route("GET /api/v1/tracks/{id}", a.getTrack)

	// ZAT push playback to terminals. GET play status is dispatched through the
	// {sub} wildcard below: a literal "/nodes/{id}/play" would ambiguity-conflict
	// with "/nodes/token/{token}" (neither is a subset of the other).
	a.route("POST /api/v1/nodes/{id}/play", a.playTrack)
	a.route("POST /api/v1/nodes/{id}/stop", a.stopPlay)
	a.route("POST /api/v1/nodes/{id}/device", a.setNodeDevice)
	// Per-node playback queue (GET goes through the {sub} wildcard).
	a.route("PUT /api/v1/nodes/{id}/queue", a.setNodeQueue)
	a.route("POST /api/v1/nodes/{id}/queue/enqueue", a.enqueueTrack)
	a.route("POST /api/v1/nodes/{id}/queue/next", a.queueNext)
	a.route("POST /api/v1/nodes/{id}/queue/prev", a.queuePrev)
	a.route("POST /api/v1/nodes/{id}/queue/jump", a.queueJump)

	// Root-level distribution downloads and self metrics.
	a.route("GET /dl/{name}", a.downloadDist)
	a.route("GET /metrics/server", a.serverMetrics)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "hifihub-server",
		"version":        metrics.Version,
		"signature_mode": a.cfg.SignatureMode,
		"play_gate":      a.cfg.PlayGateEnabled,
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	TraceID string `json:"trace_id"`
}

func writeErr(w http.ResponseWriter, status int, code, message, detail string) {
	writeJSON(w, status, apiError{Code: code, Message: message, Detail: detail, TraceID: uuid.NewString()})
}

// fail maps service errors to HTTP responses.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", err.Error(), "")
	case errors.Is(err, license.ErrInactive), errors.Is(err, task.ErrNodeUnauthorized):
		writeErr(w, http.StatusConflict, "license_required", err.Error(), "")
	default:
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error(), "")
	}
}

// writeError emits the compact {error: message} shape used by management APIs.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// clientIP extracts the remote host without the port.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// statusRecorder captures the response status for the audit middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// withAudit records every mutation (non-GET/HEAD). Detail carries only the
// method and path — never request bodies — so secrets are never logged.
func (a *API) withAudit(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.aud == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, r)
		actor := r.Header.Get("X-Actor")
		if actor == "" {
			actor = "admin"
		}
		target := r.PathValue("id")
		if target == "" {
			target = r.URL.Path
		}
		result := "ok"
		if rec.status >= 400 {
			result = "err"
		}
		e := domain.Audit{
			Actor:      actor,
			IP:         clientIP(r),
			Action:     r.Method,
			Target:     target,
			Detail:     r.Method + " " + r.URL.Path,
			Result:     result,
			DurationMS: time.Since(start).Milliseconds(),
		}
		if err := a.aud.Write(e); err != nil {
			slog.Warn("audit write failed", "err", err)
		}
	}
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
