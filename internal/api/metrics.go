package api

import "net/http"

func (a *API) serverMetrics(w http.ResponseWriter, _ *http.Request) {
	if a.met == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "metrics_unavailable", "metrics unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.met.Snapshot(a.cfg.SignatureMode, a.cfg.PlayGateEnabled))
}
