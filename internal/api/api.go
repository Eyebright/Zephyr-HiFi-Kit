// Package api exposes the REST surface (FR-9.2). Realtime WS and mTLS are
// layered on in a later increment; this file wires the JSON HTTP handlers.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/alerts"
	"hifihub/server/internal/aptsrc"
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
	AptSrc   *aptsrc.Service
	Rollouts *rollouts.Service
	Metrics  *metrics.Collector
	Library  *library.Service
	ZAT      *zatserver.Server
	Update   *update.Service
}

// API aggregates the services and the router.
type API struct {
	seatMu sync.Mutex
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
	apt    *aptsrc.Service
	rolls  *rollouts.Service
	met    *metrics.Collector
	lib    *library.Service
	zat    *zatserver.Server
	upd    *update.Service
	admin  *settingsAdmin
	mux    *http.ServeMux

	sshMu   sync.Mutex
	sshJobs map[string]*sshInstallJob
	sshSem  chan struct{}
}

// New builds the API and registers routes.
func New(cfg config.Config, d Deps) *API {
	a := &API{
		cfg: cfg, cat: d.Cat, repo: d.Repo, boms: d.Boms, nodes: d.Nodes, tasks: d.Tasks,
		lic: d.License, keys: d.Keys, aud: d.Audit, alerts: d.Alerts, cmds: d.Commands, apt: d.AptSrc,
		rolls: d.Rollouts, met: d.Metrics, lib: d.Library, zat: d.ZAT, upd: d.Update, mux: http.NewServeMux(),
		sshJobs: map[string]*sshInstallJob{}, sshSem: make(chan struct{}, 2),
	}
	a.admin = newSettingsAdmin(cfg)
	if err := a.initializeSettingsProfile(); err != nil {
		slog.Error("settings profile initialization failed", "err", err)
	}
	a.routes()
	return a
}

// Handler returns the root HTTP handler.
func (a *API) Handler() http.Handler { return a.mux }

// route registers a handler; every mutation is wrapped with the audit log.
func (a *API) route(pattern string, h http.HandlerFunc) {
	switch pattern {
	case "POST /api/v1/mounts", "PATCH /api/v1/mounts/{id}", "DELETE /api/v1/mounts/{id}",
		"POST /api/v1/mounts/{id}/mount", "POST /api/v1/mounts/{id}/unmount", "POST /api/v1/mounts/{id}/scan", "POST /api/v1/mounts/{id}/test", "POST /api/v1/mounts/{id}/archive",
		"POST /api/v1/license/activate", "POST /api/v1/keys/import", "POST /api/v1/keys/{id}/trust-root",
		"POST /api/v1/apps", "PATCH /api/v1/apps/{id}", "POST /api/v1/apps/{id}/versions", "POST /api/v1/apps/{id}/versions/import-local",
		"POST /api/v1/apps/{id}/versions/{ver}/{arch}/ban", "POST /api/v1/apps/{id}/versions/{ver}/{arch}/lifecycle",
		"POST /api/v1/apt-sources", "DELETE /api/v1/apt-sources/{id}",
		"POST /api/v1/nodes/token", "POST /api/v1/nodes/restore", "PATCH /api/v1/nodes/{id}", "DELETE /api/v1/nodes/{id}",
		"POST /api/v1/nodes/ssh/probe", "POST /api/v1/nodes/ssh/install", "GET /api/v1/nodes/ssh/jobs/{id}",
		"POST /api/v1/nodes/{id}/authorize", "POST /api/v1/nodes/{id}/revoke",
		"POST /api/v1/nodes/{id}/commands", "POST /api/v1/nodes/{id}/apt-sources/apply",
		"POST /api/v1/nodes/{id}/upgrade", "POST /api/v1/nodes/{id}/server-endpoint",
		"/api/v1/nodes/{id}/console/{rest...}", "/api/v1/nodes/{id}/local/api/{rest...}",
		"POST /api/v1/boms", "POST /api/v1/boms/import", "POST /api/v1/boms/{id}/versions/{ver}/publish", "POST /api/v1/boms/{id}/rollback",
		"POST /api/v1/jobs", "POST /api/v1/jobs/{id}/advance",
		"POST /api/v1/rollouts", "POST /api/v1/rollouts/{id}/advance", "DELETE /api/v1/rollouts/{id}",
		"POST /api/v1/update/check", "POST /api/v1/update/apply", "POST /api/v1/update/auto",
		"POST /api/v1/update/fleet/policy", "POST /api/v1/update/fleet/converge", "POST /api/v1/update/fleet/{id}/retry",
		"POST /api/v1/alerts/{id}/ack", "POST /api/v1/alerts/{id}/resolve", "POST /api/v1/alerts/ack-all", "POST /api/v1/alerts/scan":
		h = a.requireSettingsAdmin(h)
	}
	a.mux.HandleFunc(pattern, a.withAudit(h))
}

func (a *API) routes() {
	a.route("GET /api/v1/health", a.health)
	a.route("GET /api/v1/discover", a.discoverNode)
	a.route("GET /api/v1/settings/session", a.settingsSession)
	a.route("POST /api/v1/settings/setup", a.setupSettingsAdmin)
	a.route("POST /api/v1/settings/login", a.loginSettingsAdmin)
	a.route("POST /api/v1/settings/logout", a.logoutSettingsAdmin)
	a.route("GET /api/v1/settings/profile", a.getSettingsProfile)
	a.route("PATCH /api/v1/settings/profile", a.updateSettingsProfile)
	a.registerSettingsLanguageRoutes()
	a.route("PATCH /api/v1/settings/onboarding", a.updateSettingsOnboarding)
	a.route("GET /api/v1/settings/maintenance", a.settingsMaintenance)
	a.route("GET /api/v1/settings/maintenance/text-preview", a.settingsTextRepairPreview)
	a.route("POST /api/v1/settings/maintenance/text-repair", a.settingsTextRepairApply)
	a.route("POST /api/v1/settings/backup", a.exportSettingsBackup)
	a.route("POST /api/v1/settings/restore", a.withLibraryMaintenance(a.restoreSettingsBackup))
	a.route("POST /api/v1/settings/cleanup", a.withLibraryMaintenance(a.cleanupSettingsLibrary))

	// Catalog (FR-3.1)
	a.route("GET /api/v1/apps", a.listApps)
	a.route("POST /api/v1/apps", a.createApp)
	a.route("GET /api/v1/apps/{id}", a.getApp)
	a.route("PATCH /api/v1/apps/{id}", a.updateApp)
	a.route("GET /api/v1/apps/{id}/versions", a.listVersions)
	a.route("POST /api/v1/apps/{id}/versions", a.addVersion)
	a.route("POST /api/v1/apps/{id}/versions/import-local", a.importLocalVersion)
	a.route("POST /api/v1/apps/{id}/versions/{ver}/{arch}/ban", a.banVersion)
	a.route("POST /api/v1/apps/{id}/versions/{ver}/{arch}/lifecycle", a.lifecycleVersion)

	// Open-source APT delivery (phase 2)
	a.route("GET /api/v1/apt-apps", a.listAptApps)
	a.route("GET /api/v1/apt-sources", a.listAptSources)
	a.route("POST /api/v1/apt-sources", a.createAptSource)
	a.route("DELETE /api/v1/apt-sources/{id}", a.deleteAptSource)

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
	// SSH-assisted onboarding: probe confirms the host key before install.
	a.route("POST /api/v1/nodes/ssh/probe", a.sshProbe)
	a.route("POST /api/v1/nodes/ssh/install", a.sshInstall)
	a.route("GET /api/v1/nodes/ssh/jobs/{id}", a.sshInstallStatus)
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
	a.route("POST /api/v1/nodes/{id}/upgrade", a.nodeUpgrade)
	a.route("POST /api/v1/nodes/{id}/apt-sources/apply", a.applyAptSources)
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

	// Media library: SMB mounts + scanned tracks (Zephyr HiFi Kit).
	a.route("GET /api/v1/mounts", a.listMounts)
	a.route("POST /api/v1/mounts", a.addMount)
	a.route("GET /api/v1/mounts/{id}", a.getMount)
	a.route("PATCH /api/v1/mounts/{id}", a.patchMount)
	a.route("POST /api/v1/mounts/{id}/test", a.testMount)
	a.route("DELETE /api/v1/mounts/{id}", a.deleteMount)
	a.route("POST /api/v1/mounts/{id}/mount", a.mountShare)
	a.route("POST /api/v1/mounts/{id}/unmount", a.unmountShare)
	a.route("POST /api/v1/mounts/{id}/scan", a.scanMount)
	a.route("POST /api/v1/mounts/{id}/archive", a.archiveMount)
	a.route("GET /api/v1/library/browse", a.browseLibrary)
	a.route("GET /api/v1/library/related", a.relatedAlbums)
	a.route("GET /api/v1/library/select", a.selectLibraryTracks)
	a.route("GET /api/v1/library/artist-index", a.artistIndex)
	a.route("GET /api/v1/tracks", a.listTracks)
	a.route("GET /api/v1/tracks/{id}", a.getTrack)
	a.route("GET /api/v1/covers/{hash}", a.getCover)
	a.route("GET /api/v1/covers/{hash}/{size}", a.getCoverSized)

	// ZAT push playback to terminals. GET play status is dispatched through the
	// {sub} wildcard below: a literal "/nodes/{id}/play" would ambiguity-conflict
	// with "/nodes/token/{token}" (neither is a subset of the other).
	a.route("POST /api/v1/nodes/{id}/play", a.playTrack)
	a.route("POST /api/v1/nodes/{id}/stop", a.stopPlay)
	a.route("POST /api/v1/nodes/{id}/pause", a.pausePlay)
	a.route("POST /api/v1/nodes/{id}/resume", a.resumePlay)
	a.route("POST /api/v1/nodes/{id}/seek", a.seekPlay)
	a.route("POST /api/v1/nodes/{id}/volume", a.setNodeVolume)
	a.route("POST /api/v1/nodes/{id}/device", a.setNodeDevice)
	// Sleep timer stops playback at a deadline; per-node and persisted.
	a.route("PUT /api/v1/nodes/{id}/sleep", a.setSleepTimer)
	a.route("DELETE /api/v1/nodes/{id}/sleep", a.cancelSleepTimer)
	// Per-node playback queue (GET goes through the {sub} wildcard).
	a.route("PUT /api/v1/nodes/{id}/queue", a.setNodeQueue)
	a.route("POST /api/v1/nodes/{id}/queue/enqueue", a.enqueueTrack)
	a.route("POST /api/v1/nodes/{id}/queue/batch", a.enqueueTracks)
	a.route("POST /api/v1/nodes/{id}/queue/next", a.queueNext)
	a.route("POST /api/v1/nodes/{id}/queue/prev", a.queuePrev)
	a.route("POST /api/v1/nodes/{id}/queue/jump", a.queueJump)
	a.route("POST /api/v1/nodes/{id}/queue/mode", a.setQueueMode)
	a.route("POST /api/v1/nodes/{id}/server-endpoint", a.pushServerEndpoint)

	// Playlists + play history (music-centric console).
	a.route("GET /api/v1/history", a.listHistory)
	a.route("GET /api/v1/listening", a.listListening)
	a.route("GET /api/v1/favorites", a.listFavorites)
	a.route("GET /api/v1/favorites/albums", a.listFavoriteAlbums)
	a.route("GET /api/v1/favorites/state", a.favoriteState)
	a.route("PUT /api/v1/favorites", a.saveFavorite)
	a.route("DELETE /api/v1/favorites", a.deleteFavorite)
	a.route("GET /api/v1/playlists", a.listPlaylists)
	a.route("POST /api/v1/playlists", a.createPlaylist)
	a.route("GET /api/v1/playlists/{id}", a.getPlaylist)
	a.route("PATCH /api/v1/playlists/{id}", a.renamePlaylist)
	a.route("DELETE /api/v1/playlists/{id}", a.deletePlaylist)
	a.route("POST /api/v1/playlists/{id}/items", a.addPlaylistItems)
	a.route("PUT /api/v1/playlists/{id}/items", a.setPlaylistItems)
	a.route("DELETE /api/v1/playlists/{id}/items/{pos}", a.removePlaylistItem)
	a.route("POST /api/v1/playlists/{id}/push", a.pushPlaylist)

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
		"platform":       runtime.GOOS,
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	Error         *string        `json:"error,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	TraceID       string         `json:"trace_id"`
	MessageCode   string         `json:"message_code"`
	MessageParams map[string]any `json:"message_params,omitempty"`
}

func newAPIError(code, message, detail string) apiError {
	return apiError{Code: code, Message: message, Detail: detail, TraceID: uuid.NewString(), MessageCode: "errors." + code}
}

// The original diagnostic strings and legacy codes are never translated.
func writeErr(w http.ResponseWriter, status int, code, message, detail string) {
	writeJSON(w, status, newAPIError(code, message, detail))
}

// fail preserves the legacy status/code contract while giving known service
// errors a more specific localized summary, including wrapped sentinel errors.
func fail(w http.ResponseWriter, err error, fallback ...string) {
	status, code := http.StatusBadRequest, "bad_request"
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, license.ErrInactive), errors.Is(err, task.ErrNodeUnauthorized):
		status, code = http.StatusConflict, "license_required"
	}
	body := newAPIError(code, err.Error(), "")
	if len(fallback) > 0 && fallback[0] != "" {
		code = fallback[0]
	}
	body.MessageCode = "errors." + serviceErrorCode(err, code)
	var input *inputError
	if errors.As(err, &input) {
		body.MessageParams = input.params
	}
	writeJSON(w, status, body)
}

// writeError keeps the management API's original error field and adds the
// standard envelope. Callers with product-owned text use writeErrorCode;
// callers holding an error use writeServiceError before losing its identity.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeErrorCode(w, status, statusErrorCode(status), msg)
}

func writeErrorCode(w http.ResponseWriter, status int, code, msg string, params ...map[string]any) {
	if code == "" {
		code = statusErrorCode(status)
	}
	body := newAPIError(code, msg, "")
	body.Error = &body.Message
	if len(params) > 0 {
		body.MessageParams = params[0]
	}
	writeJSON(w, status, body)
}

func writeServiceError(w http.ResponseWriter, status int, err error, fallback ...string) {
	code := statusErrorCode(status)
	if len(fallback) > 0 && fallback[0] != "" {
		code = fallback[0]
	}
	var params map[string]any
	var input *inputError
	if errors.As(err, &input) {
		params = input.params
	}
	writeErrorCode(w, status, serviceErrorCode(err, code), err.Error(), params)
}

func statusErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout:
		return "request_timeout"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusGatewayTimeout:
		return "gateway_timeout"
	default:
		return "internal_error"
	}
}

// inputError carries authored validation metadata without changing its legacy
// diagnostic string. Parameter values remain data, never HTML or translation keys.
type inputError struct {
	code, diagnostic string
	params           map[string]any
}

func (e *inputError) Error() string { return e.diagnostic }

func invalidInput(code, diagnostic string, params ...map[string]any) error {
	err := &inputError{code: code, diagnostic: diagnostic}
	if len(params) > 0 {
		err.params = params[0]
	}
	return err
}

// Classify by error identity only, never by matching translated sentences or
// user-supplied text. Unknown errors retain an operation/status-level summary.
func serviceErrorCode(err error, fallback string) string {
	var input *inputError
	if errors.As(err, &input) {
		return input.code
	}
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return "not_found"
	case errors.Is(err, license.ErrInactive):
		return "license_required"
	case errors.Is(err, task.ErrNodeUnauthorized):
		return "node_unauthorized"
	case errors.Is(err, node.ErrPlaying):
		return "node_playing"
	case errors.Is(err, commands.ErrPlaying):
		return "upgrade_playing"
	case errors.Is(err, commands.ErrPlayingInstall):
		return "install_playing"
	case errors.Is(err, update.ErrBusy):
		return "update_busy"
	case errors.Is(err, rollouts.ErrRingBusy):
		return "rollout_ring_busy"
	case errors.Is(err, zatserver.ErrNotConnected):
		return "zat_not_connected"
	case errors.Is(err, zatserver.ErrUnknownDevice):
		return "unknown_audio_device"
	case errors.Is(err, zatserver.ErrNoSession):
		return "no_playback_session"
	case errors.Is(err, zatserver.ErrQueueEmpty):
		return "queue_empty"
	case errors.Is(err, zatserver.ErrQueueEnd):
		return "queue_end"
	case errors.Is(err, zatserver.ErrQueueIndex):
		return "queue_index_invalid"
	case errors.Is(err, zatserver.ErrMQACapability):
		return "mqa_unsupported"
	case errors.Is(err, zatserver.ErrMQADevice):
		return "mqa_device_requires_hw"
	case errors.Is(err, zatserver.ErrMQALocked):
		return "mqa_volume_locked"
	case errors.Is(err, zatserver.ErrDoPCapability):
		return "dop_unsupported"
	case errors.Is(err, zatserver.ErrDoPDevice):
		return "dop_device_requires_hw"
	case errors.Is(err, zatserver.ErrDoPLocked):
		return "dop_volume_locked"
	case errors.Is(err, library.ErrNotMounted):
		return "source_not_mounted"
	case errors.Is(err, library.ErrLibraryBusy):
		return "library_busy"
	case errors.Is(err, library.ErrInvalidLibraryBackup):
		return "backup_invalid"
	case errors.Is(err, library.ErrInvalidLibraryCleanup):
		return "cleanup_invalid"
	case errors.Is(err, library.ErrLibraryMaintenance):
		return "maintenance_failed"
	default:
		return fallback
	}
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
