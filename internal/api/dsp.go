package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/dsp"
	"hifihub/server/internal/zatserver"
)

// --- per-device DSP / EQ ---

func (a *API) dspPresets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, dsp.Presets)
}

func (a *API) nodeDeviceDSP(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	id := r.PathValue("id")
	cfg, bypass, err := a.zat.DeviceDSP(id, id+"::"+r.PathValue("hw"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		dsp.Config
		Bypass bool `json:"bypass"`
	}{cfg, bypass})
}

func (a *API) setNodeDeviceDSP(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var cfg dsp.Config
	if err := decode(r, &cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	id := r.PathValue("id")
	if err := a.zat.SetDeviceDSP(id, id+"::"+r.PathValue("hw"), cfg); err != nil {
		if errors.Is(err, zatserver.ErrInvalidDeviceKey) {
			writeServiceError(w, http.StatusBadRequest, err)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}