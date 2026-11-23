package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"hifihub/server/internal/library"
	"hifihub/server/internal/zatserver"
)

// --- SMB mounts ---

func (a *API) listMounts(w http.ResponseWriter, _ *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
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
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	var req library.AddMountRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	m, err := a.lib.AddMount(req)
	if err != nil {
		code := "source_add_failed"
		if req.Kind == "local" {
			code = "local_source_invalid"
		}
		fail(w, err, code)
		return
	}
	status := http.StatusCreated
	if m.Reused {
		status = http.StatusOK
	}
	writeJSON(w, status, m)
}

func (a *API) getMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
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
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
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
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	m, err := a.lib.Mount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err, "source_mount_failed")
		return
	}
	writeMountResult(w, m, "source_mount_failed")
}

func (a *API) unmountShare(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	m, err := a.lib.Unmount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err, "source_unmount_failed")
		return
	}
	writeMountResult(w, m, "source_unmount_failed")
}

// A mount operation may return HTTP 200 with status=error. Keep its persisted
// detail verbatim and add only an operation-level summary to this API response.
func writeMountResult(w http.ResponseWriter, m library.Mount, code string) {
	detailCode := ""
	if m.Status == library.StatusError {
		detailCode = "errors." + code
	}
	writeJSON(w, http.StatusOK, struct {
		library.Mount
		DetailCode string `json:"detail_code,omitempty"`
	}{m, detailCode})
}

func (a *API) scanMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "" && mode != "update" && mode != "full" {
		writeErrorCode(w, http.StatusBadRequest, "scan_mode_invalid", "扫描模式必须为 update 或 full")
		return
	}
	result, err := a.lib.ScanChanges(r.Context(), r.PathValue("id"), mode == "full")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) archiveMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	m, err := a.lib.ArchiveMount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// --- tracks ---

func (a *API) listTracks(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
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
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	t, err := a.lib.GetTrack(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// getCover serves content-addressed album art; hashes are immutable so clients
// may cache forever.
func (a *API) getCover(w http.ResponseWriter, r *http.Request) {
	// Also protect legacy cached contents that predate raster validation.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	mime, data, err := a.lib.GetCover(r.PathValue("hash"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// getCoverSized serves a lazily rendered thumbnail (96/256/640 px long edge).
func (a *API) getCoverSized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	size, err := strconv.Atoi(r.PathValue("size"))
	if err != nil || !library.CoverSizes[size] {
		writeErrorCode(w, http.StatusBadRequest, "cover_size_invalid", "封面尺寸仅支持 96、256、384、640")
		return
	}
	mime, data, err := a.lib.GetCoverVariant(r.PathValue("hash"), size)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// --- ZAT push playback ---

func (a *API) playTrack(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
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
		writeErrorCode(w, http.StatusBadRequest, "track_id_required", "track_id 不能为空")
		return
	}
	sessionID, err := a.zat.StartPlay(r.Context(), r.PathValue("id"), body.TrackID)
	if err != nil {
		if errors.Is(err, zatserver.ErrNotConnected) {
			writeServiceError(w, http.StatusConflict, err)
			return
		}
		fail(w, err)
		return
	}
	// A direct push replaces the queue with the single track; enqueue keeps it.
	if err := a.zat.SetQueue(r.PathValue("id"), []string{body.TrackID}, 0); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID})
}

func (a *API) stopPlay(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	if err := a.zat.Stop(r.PathValue("id")); err != nil {
		if errors.Is(err, zatserver.ErrNotConnected) {
			writeServiceError(w, http.StatusConflict, err)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) playStatus(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	st, err := a.zat.Status(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) setNodeDevice(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		Device string `json:"device"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if err := a.zat.SetDevice(r.PathValue("id"), body.Device); err != nil {
		switch {
		case errors.Is(err, zatserver.ErrNotConnected):
			writeServiceError(w, http.StatusConflict, err)
		case errors.Is(err, zatserver.ErrUnknownDevice):
			writeServiceError(w, http.StatusBadRequest, err)
		case errors.Is(err, zatserver.ErrDeviceNotActivated):
			writeServiceError(w, http.StatusConflict, err)
		default:
			fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": body.Device})
}

// zatControlFail maps transport-control errors to HTTP status codes.
func zatControlFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, zatserver.ErrNotConnected), errors.Is(err, zatserver.ErrNoSession):
		writeServiceError(w, http.StatusConflict, err)
	default:
		fail(w, err)
	}
}

func (a *API) pausePlay(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	if err := a.zat.Pause(r.PathValue("id")); err != nil {
		zatControlFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "paused"})
}

func (a *API) resumePlay(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	if err := a.zat.Resume(r.PathValue("id")); err != nil {
		zatControlFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "playing"})
}

func (a *API) seekPlay(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		PositionMS int `json:"position_ms"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if body.PositionMS < 0 {
		writeErrorCode(w, http.StatusBadRequest, "seek_position_invalid", "position_ms 不能为负")
		return
	}
	id := r.PathValue("id")
	st, err := a.zat.Status(id)
	if err != nil {
		fail(w, err)
		return
	}
	if st.Rate <= 0 {
		writeErrorCode(w, http.StatusConflict, "no_seekable_session", "当前没有可定位的播放会话")
		return
	}
	samples := uint64(body.PositionMS) * uint64(st.Rate) / 1000
	if err := a.zat.Seek(id, samples); err != nil {
		zatControlFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pts": samples})
}

func (a *API) setNodeVolume(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		Volume int `json:"volume"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if body.Volume < 0 || body.Volume > 100 {
		writeErrorCode(w, http.StatusBadRequest, "volume_invalid", "volume 须在 0..100")
		return
	}
	if err := a.zat.SetVolume(r.PathValue("id"), body.Volume); err != nil {
		zatControlFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "volume": body.Volume})
}

// --- per-node playback queue ---

func (a *API) nodeQueue(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	q, err := a.zat.Queue(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (a *API) setNodeQueue(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		TrackIDs   []string `json:"track_ids"`
		Position   int      `json:"position"`
		PlayIfIdle bool     `json:"play_if_idle"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	var err error
	if body.PlayIfIdle {
		_, err = a.zat.ReplaceQueue(r.Context(), r.PathValue("id"), body.TrackIDs, body.Position)
	} else {
		err = a.zat.SetQueue(r.PathValue("id"), body.TrackIDs, body.Position)
	}
	if err != nil {
		fail(w, err)
		return
	}
	q, err := a.zat.Queue(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (a *API) enqueueTrack(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
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
		writeErrorCode(w, http.StatusBadRequest, "track_id_required", "track_id 不能为空")
		return
	}
	q, sessionID, err := a.zat.Enqueue(r.Context(), r.PathValue("id"), body.TrackID)
	if err != nil {
		if errors.Is(err, zatserver.ErrNotConnected) {
			writeServiceError(w, http.StatusConflict, err)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": q, "session_id": sessionID})
}

func (a *API) enqueueTracks(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		TrackIDs []string `json:"track_ids"`
		Next     bool     `json:"next"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	q, sessionID, err := a.zat.EnqueueMany(r.Context(), r.PathValue("id"), body.TrackIDs, body.Next)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": q, "session_id": sessionID})
}

func (a *API) queueMove(w http.ResponseWriter, r *http.Request, move func(context.Context, string) (string, error)) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	sessionID, err := move(r.Context(), r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, zatserver.ErrNotConnected):
			writeServiceError(w, http.StatusConflict, err)
		case errors.Is(err, zatserver.ErrQueueEmpty), errors.Is(err, zatserver.ErrQueueEnd), errors.Is(err, zatserver.ErrQueueIndex):
			writeServiceError(w, http.StatusBadRequest, err)
		default:
			fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID})
}

func (a *API) queueNext(w http.ResponseWriter, r *http.Request) {
	a.queueMove(w, r, a.zat.QueueNext)
}

func (a *API) queuePrev(w http.ResponseWriter, r *http.Request) {
	a.queueMove(w, r, a.zat.QueuePrev)
}

func (a *API) queueJump(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		Index int `json:"index"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	a.queueMove(w, r, func(ctx context.Context, nodeID string) (string, error) {
		return a.zat.QueueJump(ctx, nodeID, body.Index)
	})
}

// setQueueMode persists the terminal's queue playback mode ("sequence",
// "loop_all", "loop_one" or "shuffle") and returns the updated queue.
func (a *API) setQueueMode(w http.ResponseWriter, r *http.Request) {
	if a.zat == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "zat_unavailable", "ZAT 服务未启用")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if !zatserver.ValidQueueMode(body.Mode) {
		writeErrorCode(w, http.StatusBadRequest, "bad_request", "队列播放模式无效："+body.Mode)
		return
	}
	q, err := a.zat.SetQueueMode(r.PathValue("id"), body.Mode)
	if err != nil {
		if errors.Is(err, zatserver.ErrQueueMode) {
			writeServiceError(w, http.StatusBadRequest, err)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}
