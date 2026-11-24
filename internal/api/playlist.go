package api

import (
	"net/http"
	"strconv"

	"hifihub/server/internal/library"
)

// --- play history ---

func (a *API) listHistory(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hs, err := a.lib.ListHistory(r.URL.Query().Get("node_id"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hs)
}

// --- playlists ---

func (a *API) listPlaylists(w http.ResponseWriter, _ *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	ps, err := a.lib.ListPlaylists()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (a *API) createPlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	p, err := a.lib.CreatePlaylist(body.Name)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) getPlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	d, err := a.lib.GetPlaylist(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) renamePlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if err := a.lib.RenamePlaylist(r.PathValue("id"), body.Name); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	if err := a.lib.DeletePlaylist(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) addPlaylistItems(w http.ResponseWriter, r *http.Request) {
	a.playlistItems(w, r, false)
}

func (a *API) setPlaylistItems(w http.ResponseWriter, r *http.Request) {
	a.playlistItems(w, r, true)
}

func (a *API) playlistItems(w http.ResponseWriter, r *http.Request, replace bool) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	var body struct {
		TrackIDs []string `json:"track_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	var d library.PlaylistDetail
	var err error
	if replace {
		d, err = a.lib.SetPlaylistItems(r.PathValue("id"), body.TrackIDs)
	} else {
		d, err = a.lib.AddPlaylistItems(r.PathValue("id"), body.TrackIDs)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) removePlaylistItem(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	pos, err := strconv.Atoi(r.PathValue("pos"))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "playlist_position_invalid", "位置无效")
		return
	}
	d, err := a.lib.RemovePlaylistItem(r.PathValue("id"), pos)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// pushPlaylist loads the playlist into the target playback device's queue and
// starts playback at the first entry. Like a direct push, it replaces the queue.
func (a *API) pushPlaylist(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil || a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "playback_unavailable", "媒体库或 ZAT 服务未启用")
		return
	}
	var body struct {
		NodeID    string `json:"node_id"`
		DeviceKey string `json:"device_key"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if body.NodeID == "" {
		writeErrorCode(w, http.StatusBadRequest, "node_id_required", "node_id 不能为空")
		return
	}
	d, err := a.lib.GetPlaylist(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if len(d.Items) == 0 {
		writeErrorCode(w, http.StatusBadRequest, "playlist_empty", "播放列表为空")
		return
	}
	ids := make([]string, 0, len(d.Items))
	for _, it := range d.Items {
		if it.Missing {
			writeErrorCode(w, http.StatusBadRequest, "playlist_track_missing", "播放列表包含已缺失的曲目，请先移除："+it.TrackID, map[string]any{"id": it.TrackID})
			return
		}
		ids = append(ids, it.TrackID)
	}
	sessionID, err := a.zat.StartPlayDevice(r.Context(), body.NodeID, ids[0], body.DeviceKey)
	if err != nil {
		zatControlFail(w, err)
		return
	}
	if err := a.zat.SetQueueDevice(body.NodeID, body.DeviceKey, ids, 0); err != nil {
		zatControlFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID, "count": len(ids)})
}
