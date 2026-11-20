package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/domain"
)

func (a *API) listAptApps(w http.ResponseWriter, _ *http.Request) {
	apps, err := a.cat.ListAptApps()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func (a *API) listAptSources(w http.ResponseWriter, _ *http.Request) {
	if a.apt == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "apt_sources_unavailable", "APT 源管理未启用")
		return
	}
	ss, err := a.apt.List()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ss)
}

func (a *API) createAptSource(w http.ResponseWriter, r *http.Request) {
	if a.apt == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "apt_sources_unavailable", "APT 源管理未启用")
		return
	}
	var req domain.AptSource
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	src, err := a.apt.Create(req)
	if err != nil {
		writeServiceError(w, http.StatusBadRequest, err, "apt_source_invalid")
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (a *API) deleteAptSource(w http.ResponseWriter, r *http.Request) {
	if a.apt == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "apt_sources_unavailable", "APT 源管理未启用")
		return
	}
	if err := a.apt.Delete(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// applyAptSources queues an apt-source command carrying every registry entry
// that matches the terminal's distro codename.
func (a *API) applyAptSources(w http.ResponseWriter, r *http.Request) {
	if a.apt == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "apt_sources_unavailable", "APT 源管理未启用")
		return
	}
	id := r.PathValue("id")
	n, err := a.nodes.Get(id)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "node_not_found", "终端不存在")
			return
		}
		fail(w, err)
		return
	}
	specs, err := a.apt.SpecsFor(n.Distro)
	if err != nil {
		fail(w, err)
		return
	}
	if len(specs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, "apt_sources_empty", "没有适用于该终端发行版的第三方 APT 源（发行版自带软件包无需额外源）")
		return
	}
	c, err := a.cmds.Create(id, domain.CommandAptSource, domain.CommandParams{Sources: specs})
	if err != nil {
		if commands.IsPlaying(err) {
			writeServiceError(w, http.StatusConflict, err)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
