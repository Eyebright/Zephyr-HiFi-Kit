package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/library"
	"hifihub/server/internal/zatserver"
)

// --- SMB mounts ---

func (a *API) listMounts(w http.ResponseWriter, _ *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	ms, err := a.lib.ListMounts()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (a *API) addMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	var req library.AddMountRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	m, err := a.lib.AddMount(req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *API) getMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	m, err := a.lib.GetMount(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) deleteMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	if err := a.lib.DeleteMount(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) mountShare(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	m, err := a.lib.Mount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) unmountShare(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	m, err := a.lib.Unmount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) scanMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	n, err := a.lib.Scan(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n})
}

// --- tracks ---

func (a *API) listTracks(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	ts, err := a.lib.ListTracks(r.URL.Query().Get("mount_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (a *API) getTrack(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	t, err := a.lib.GetTrack(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// --- ZAT push playback ---

func (a *API) playTrack(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeError(w, http.StatusServiceUnavailable, "ZAT 服务未启用")
		return
	}
	var body struct {
		TrackID string `json:"track_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if body.TrackID == "" {
		writeError(w, http.StatusBadRequest, "track_id 不能为空")
		return
	}
	sessionID, err := a.zat.StartPlay(r.Context(), r.PathValue("id"), body.TrackID)
	if err != nil {
		if errors.Is(err, zatserver.ErrNotConnected) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID})
}

func (a *API) stopPlay(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeError(w, http.StatusServiceUnavailable, "ZAT 服务未启用")
		return
	}
	if err := a.zat.Stop(r.PathValue("id")); err != nil {
		if errors.Is(err, zatserver.ErrNotConnected) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) playStatus(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeError(w, http.StatusServiceUnavailable, "ZAT 服务未启用")
		return
	}
	st, err := a.zat.Status(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
