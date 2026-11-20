package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
)

func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	as, err := a.alerts.List(r.URL.Query().Get("status"))
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func (a *API) ackAlert(w http.ResponseWriter, r *http.Request) {
	al, err := a.alerts.Ack(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeServiceError(w, http.StatusConflict, err, "alert_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (a *API) resolveAlert(w http.ResponseWriter, r *http.Request) {
	al, err := a.alerts.Resolve(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeServiceError(w, http.StatusConflict, err, "alert_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (a *API) ackAllAlerts(w http.ResponseWriter, _ *http.Request) {
	n, err := a.alerts.AckAll()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "acknowledged": n})
}

func (a *API) scanAlerts(w http.ResponseWriter, _ *http.Request) {
	n, err := a.alerts.Scan()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": n})
}
