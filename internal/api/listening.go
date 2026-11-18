package api

import (
	"net/http"
	"strconv"
)

func (a *API) listListening(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		var err error
		days, err = strconv.Atoi(raw)
		if err != nil || (days != 1 && days != 7 && days != 30) {
			writeError(w, http.StatusBadRequest, "days 必须为 1、7 或 30")
			return
		}
	}
	summary, err := a.lib.Listening(days, r.URL.Query().Get("node_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
