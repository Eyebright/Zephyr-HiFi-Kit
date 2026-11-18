package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/domain"
)

func (a *API) createCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type   string               `json:"type"`
		Params domain.CommandParams `json:"params"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	c, err := a.cmds.Create(r.PathValue("id"), body.Type, body.Params)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "终端不存在")
			return
		}
		if commands.IsPlaying(err) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) listCommands(w http.ResponseWriter, r *http.Request) {
	cs, err := a.cmds.List(r.PathValue("id"), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (a *API) commandResult(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string          `json:"status"`
		Log    string          `json:"log"`
		Data   json.RawMessage `json:"data"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	c, err := a.cmds.Result(r.PathValue("id"), body.Status, body.Log, body.Data)
	if err != nil {
		switch {
		case errors.Is(err, catalog.ErrNotFound):
			writeError(w, http.StatusNotFound, "命令不存在")
		case c.Status == "done":
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) nodeOSUpdates(w http.ResponseWriter, r *http.Request) {
	st, err := a.cmds.OSUpdates(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) nodeNetDiag(w http.ResponseWriter, r *http.Request) {
	st, err := a.cmds.NetDiag(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}
