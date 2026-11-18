package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"hifihub/server/internal/library"
)

// The router holds libraryGate while checking playback and performing maintenance.
func (a *API) settingsLibraryIdle(w http.ResponseWriter) bool {
	if a.nodes == nil {
		writeError(w, http.StatusServiceUnavailable, "无法确认节点播放状态")
		return false
	}
	nodes, err := a.nodes.List()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法确认节点播放状态")
		return false
	}
	for _, n := range nodes {
		if n.Playing {
			writeError(w, http.StatusConflict, "请先停止所有节点播放（包括暂停的会话）")
			return false
		}
		if a.zat != nil {
			state, err := a.zat.Status(n.NodeID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "无法确认节点播放状态")
				return false
			}
			if state.Playing || state.State == "playing" || state.State == "paused" || state.SessionID != "" {
				writeError(w, http.StatusConflict, "请先停止所有节点播放（包括暂停的会话）")
				return false
			}
		}
	}
	return true
}

func (a *API) settingsMaintenance(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a.lib == nil {
			writeError(w, http.StatusServiceUnavailable, "曲库不可用")
			return
		}
		stats, err := a.lib.LibraryMaintenanceStatus()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取维护状态失败")
			return
		}
		writeJSON(w, http.StatusOK, stats)
	})(w, r)
}

func (a *API) exportSettingsBackup(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a.lib == nil {
			writeError(w, http.StatusServiceUnavailable, "曲库不可用")
			return
		}
		data, err := a.lib.ExportLibrary(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "曲库备份失败，数据可能超过备份限制或关联不完整")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="zephyr-library-`+time.Now().UTC().Format("20060102-150405")+`.json"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})(w, r)
}

func (a *API) restoreSettingsBackup(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a.lib == nil {
			writeError(w, http.StatusServiceUnavailable, "曲库不可用")
			return
		}
		var req struct {
			Confirm string          `json:"confirm"`
			Backup  json.RawMessage `json:"backup"`
		}
		if err := decodeSettingsJSON(http.MaxBytesReader(w, r.Body, library.MaxLibraryBackupBytes+1024), &req); err != nil || req.Confirm != "RESTORE" || len(req.Backup) == 0 {
			writeError(w, http.StatusBadRequest, "需要 RESTORE 确认和有效曲库备份（最大64MiB）")
			return
		}
		if !a.settingsLibraryIdle(w) {
			return
		}
		if err := a.lib.RestoreLibrary(r.Context(), req.Backup); err != nil {
			if errors.Is(err, library.ErrLibraryBusy) {
				writeError(w, http.StatusConflict, "请先卸载所有 SMB 共享，并等待扫描或来源更新结束后再恢复")
				return
			}
			if errors.Is(err, library.ErrInvalidLibraryBackup) {
				writeError(w, http.StatusBadRequest, "备份格式、版本或关联数据无效")
				return
			}
			writeError(w, http.StatusInternalServerError, "恢复失败，曲库未更改")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"restored": true})
	})(w, r)
}

func (a *API) cleanupSettingsLibrary(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a.lib == nil {
			writeError(w, http.StatusServiceUnavailable, "曲库不可用")
			return
		}
		var req struct {
			Kind    string `json:"kind"`
			Before  int64  `json:"before"`
			Confirm string `json:"confirm"`
		}
		if err := decodeSettingsJSON(http.MaxBytesReader(w, r.Body, 4096), &req); err != nil || req.Confirm != "CLEAN" {
			writeError(w, http.StatusBadRequest, "需要 CLEAN 确认和有效清理参数")
			return
		}
		if req.Kind == "catalog" && !a.settingsLibraryIdle(w) {
			return
		}
		n, err := a.lib.CleanLibrary(r.Context(), req.Kind, req.Before)
		if err != nil {
			if errors.Is(err, library.ErrLibraryBusy) {
				writeError(w, http.StatusConflict, "曲库正在扫描或更新来源，请稍后再试")
				return
			}
			if errors.Is(err, library.ErrInvalidLibraryCleanup) {
				writeError(w, http.StatusBadRequest, "清理类型无效，或审计截止时间必须大于0且严格早于今天")
				return
			}
			writeError(w, http.StatusInternalServerError, "清理失败，数据未更改")
			return
		}
		// "audit" means database audit records only, never filesystem logs.
		writeJSON(w, http.StatusOK, map[string]any{"kind": req.Kind, "deleted": n})
	})(w, r)
}
