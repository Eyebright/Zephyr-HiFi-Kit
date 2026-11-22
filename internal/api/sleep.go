package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/zatserver"
)

// setSleepTimer arms a per-node timer that stops playback at the deadline.
func (a *API) setSleepTimer(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		Minutes int `json:"minutes"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	stopAt, err := a.zat.SetSleepTimer(r.PathValue("id"), body.Minutes)
	if err != nil {
		if errors.Is(err, zatserver.ErrSleepMinutes) {
			writeErrorCode(w, http.StatusBadRequest, "sleep_minutes_invalid", "定时时长需为 1 到 1440 分钟", map[string]any{"field": "minutes"})
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stop_at": stopAt})
}

// cancelSleepTimer clears the node's timer; cancelling when unset succeeds.
func (a *API) cancelSleepTimer(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	if err := a.zat.CancelSleepTimer(r.PathValue("id")); err != nil {
		fail(w, err, "sleep_cancel_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
