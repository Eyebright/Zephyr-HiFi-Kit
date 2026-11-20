package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/update"
)

// updateStatus reports the server self-update state and the fleet policy. The
// console polls this alongside /update/fleet to render the convergence panel.
func (a *API) updateStatus(w http.ResponseWriter, _ *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server": a.upd.ServerStatus(),
		"policy": a.upd.Policy(),
	})
}

// updateCheck polls the release feed now. The returned state encodes the outcome
// (idle/offline/failed), so an unreachable feed is a 200 with state=offline.
func (a *API) updateCheck(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	st, _ := a.upd.Check(r.Context())
	writeJSON(w, http.StatusOK, st)
}

// updateApply stages and applies the newest release bundle, then restarts. It is
// play-gated: a busy fleet yields 409 and the poller retries when idle.
func (a *API) updateApply(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	st, err := a.upd.Apply(r.Context())
	if err != nil {
		if errors.Is(err, update.ErrBusy) {
			writeServiceError(w, http.StatusConflict, err)
			return
		}
		writeServiceError(w, http.StatusBadRequest, err, "update_apply_failed")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// updateAuto toggles unattended self-upgrade.
func (a *API) updateAuto(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	var body struct {
		AutoApply bool `json:"auto_apply"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	writeJSON(w, http.StatusOK, a.upd.SetAutoApply(body.AutoApply))
}

// updateFleet returns the terminal convergence snapshot without issuing commands.
func (a *API) updateFleet(w http.ResponseWriter, _ *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	writeJSON(w, http.StatusOK, a.upd.FleetStatus())
}

// updateFleetPolicy validates and persists the convergence policy.
func (a *API) updateFleetPolicy(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	var body domain.FleetUpdatePolicy
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	p, err := a.upd.SetPolicy(body)
	if err != nil {
		writeServiceError(w, http.StatusBadRequest, err, "update_policy_invalid")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// updateFleetConverge forces one immediate convergence pass.
func (a *API) updateFleetConverge(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	writeJSON(w, http.StatusOK, a.upd.Converge(r.Context()))
}

// nodeUpgrade issues one direct agent upgrade for a terminal, independent of
// the ring policy. Play protection and package availability are enforced by the
// update service.
func (a *API) nodeUpgrade(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	st, err := a.upd.UpgradeNode(r.PathValue("id"))
	if err != nil {
		status := http.StatusConflict
		switch {
		case errors.Is(err, catalog.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, update.ErrUpgradeUnavailable):
			status = http.StatusServiceUnavailable
		case errors.Is(err, update.ErrNodeOffline), errors.Is(err, update.ErrNodeUnauthorized),
			errors.Is(err, update.ErrNodeArchUnsupported), errors.Is(err, update.ErrNodePackageMissing),
			errors.Is(err, update.ErrNodePlaying), errors.Is(err, update.ErrUpgradeInFlight),
			errors.Is(err, update.ErrNodeAlreadyConverged):
			status = http.StatusConflict
		default:
			status = http.StatusBadRequest
		}
		writeErrorCode(w, status, "node_upgrade_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// updateFleetRetry clears a suspended terminal's failure counter.
func (a *API) updateFleetRetry(w http.ResponseWriter, r *http.Request) {
	if a.upd == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "update_unavailable", "更新服务未启用")
		return
	}
	if err := a.upd.RetryNode(r.PathValue("id")); err != nil {
		writeServiceError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
