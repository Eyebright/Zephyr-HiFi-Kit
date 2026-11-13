package api

import (
	"net/http"
)

func (a *API) createJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID     string `json:"node_id"`
		BomID      string `json:"bom_id"`
		BomVersion int    `json:"bom_version"`
		Kind       string `json:"kind"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	j, err := a.tasks.CreateJob(body.NodeID, body.BomID, body.BomVersion, body.Kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) {
	js, err := a.tasks.List(r.URL.Query().Get("node_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, js)
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := a.tasks.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *API) advanceJob(w http.ResponseWriter, r *http.Request) {
	j, err := a.tasks.Advance(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *API) reportPhase(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phase  string `json:"phase"`
		State  string `json:"state"`
		Detail string `json:"detail"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	j, err := a.tasks.ReportPhase(r.PathValue("id"), body.Phase, body.State, body.Detail)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (a *API) rollbackComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Recovered bool `json:"recovered"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	j, err := a.tasks.CompleteRollback(r.PathValue("id"), body.Recovered)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}