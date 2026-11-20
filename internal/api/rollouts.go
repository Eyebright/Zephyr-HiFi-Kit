package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/rollouts"
)

func (a *API) listRollouts(w http.ResponseWriter, _ *http.Request) {
	rs, err := a.rolls.List()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (a *API) createRollout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string              `json:"name"`
		BomID      string              `json:"bom_id"`
		BomVersion int                 `json:"bom_version"`
		Rings      []rollouts.RingSpec `json:"rings"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	ro, err := a.rolls.Create(body.Name, body.BomID, body.BomVersion, body.Rings)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeServiceError(w, http.StatusBadRequest, err, "rollout_operation_failed")
		return
	}
	writeJSON(w, http.StatusCreated, ro)
}

func (a *API) getRollout(w http.ResponseWriter, r *http.Request) {
	ro, err := a.rolls.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "rollout_not_found", "灰度发布不存在")
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ro)
}

func (a *API) advanceRollout(w http.ResponseWriter, r *http.Request) {
	ro, err := a.rolls.Advance(r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, rollouts.ErrRingBusy):
			writeServiceError(w, http.StatusConflict, err)
		case errors.Is(err, catalog.ErrNotFound):
			writeErrorCode(w, http.StatusNotFound, "rollout_not_found", "灰度发布不存在")
		default:
			writeServiceError(w, http.StatusBadRequest, err, "rollout_operation_failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, ro)
}

func (a *API) deleteRollout(w http.ResponseWriter, r *http.Request) {
	if err := a.rolls.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "rollout_not_found", "灰度发布不存在")
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
