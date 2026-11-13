package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"
)

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "limit 必须是非负整数")
			return
		}
		limit = n
	}
	entries, err := a.aud.List(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) exportAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := a.aud.All()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.URL.Query().Get("format") {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)
		w.WriteHeader(http.StatusOK)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "at", "actor", "ip", "action", "target", "detail", "result", "duration_ms"})
		for _, e := range entries {
			_ = cw.Write([]string{
				e.ID, time.Unix(e.At, 0).UTC().Format(time.RFC3339), e.Actor, e.IP,
				e.Action, e.Target, e.Detail, e.Result, strconv.FormatInt(e.DurationMS, 10),
			})
		}
		cw.Flush()
	case "", "json":
		w.Header().Set("Content-Disposition", `attachment; filename="audit.json"`)
		writeJSON(w, http.StatusOK, entries)
	default:
		writeError(w, http.StatusBadRequest, "format 仅支持 csv 或 json")
	}
}
