package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/node"
)

func (a *API) issueToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	_ = decode(r, &body)
	ttl := 30 * time.Minute
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	token, exp, err := a.nodes.IssueToken(ttl)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires_at": exp})
}

func (a *API) registerNode(w http.ResponseWriter, r *http.Request) {
	var req node.RegisterRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	n, secret, err := a.nodes.Register(req, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "register_failed", err.Error(), "")
		return
	}
	// The one-time console secret is returned only here; node JSON never
	// carries it afterwards.
	writeJSON(w, http.StatusCreated, struct {
		domain.Node
		Secret string `json:"secret"`
	}{Node: n, Secret: secret})
}

func (a *API) listNodes(w http.ResponseWriter, _ *http.Request) {
	ns, err := a.nodes.List()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ns)
}

func (a *API) getNode(w http.ResponseWriter, r *http.Request) {
	n, err := a.nodes.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	var hb node.Heartbeat
	if err := decode(r, &hb); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	res, err := a.nodes.Heartbeat(r.PathValue("id"), hb)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) snapshot(w http.ResponseWriter, r *http.Request) {
	var snap node.Snapshot
	if err := decode(r, &snap); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if err := a.nodes.Snapshot(r.PathValue("id"), snap); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) nodeApps(w http.ResponseWriter, r *http.Request) {
	apps, err := a.nodes.InstalledApps(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

// nodeSubresource dispatches GET /nodes/{id}/{sub} to the matching handler.
// Folding the literal sub-resource routes into one single-segment wildcard
// avoids a ServeMux ambiguity panic against GET /nodes/token/{token}, while the
// more-specific token route still takes precedence for /nodes/token/{token}.
func (a *API) nodeSubresource(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("sub") {
	case "apps":
		a.nodeApps(w, r)
	case "osupdates":
		a.nodeOSUpdates(w, r)
	case "netdiag":
		a.nodeNetDiag(w, r)
	case "commands":
		a.listCommands(w, r)
	case "play":
		a.playStatus(w, r)
	case "queue":
		a.nodeQueue(w, r)
	default:
		writeError(w, http.StatusNotFound, "未知的终端子资源")
	}
}

func (a *API) patchNode(w http.ResponseWriter, r *http.Request) {
	var p node.Patch
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	// A bound BOM must exist and be published; default to its latest version.
	if p.BomID != nil && *p.BomID != "" {
		var b domain.Bom
		var err error
		if p.BomVersion != nil && *p.BomVersion > 0 {
			b, err = a.boms.Get(*p.BomID, *p.BomVersion)
		} else {
			b, err = a.boms.Latest(*p.BomID)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "BOM 不存在："+*p.BomID)
			return
		}
		if b.Status != domain.BomPublished {
			writeError(w, http.StatusConflict, "BOM 未发布，不能绑定到终端")
			return
		}
		if p.BomVersion == nil || *p.BomVersion == 0 {
			v := b.Version
			p.BomVersion = &v
		}
	}
	n, err := a.nodes.Patch(r.PathValue("id"), p)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request) {
	n, err := a.nodes.Delete(r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, node.ErrPlaying):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, catalog.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if a.rolls != nil {
		if err := a.rolls.StripNode(n.NodeID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
}

func (a *API) restoreNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node domain.Node `json:"node"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if body.Node.NodeID == "" {
		writeError(w, http.StatusBadRequest, "node_id 不能为空")
		return
	}
	n, err := a.nodes.Restore(body.Node)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": n})
}

func (a *API) authorizeNode(w http.ResponseWriter, r *http.Request) {
	seats, licensed, err := a.lic.SeatLimit()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	n, err := a.nodes.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !n.Authorized {
		used, err := a.nodes.AuthorizedCount()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if used >= seats {
			msg := fmt.Sprintf("内置授权席位已满（%d/%d），激活许可证可授权更多终端", used, seats)
			if licensed {
				msg = fmt.Sprintf("许可证席位已满（%d/%d），无法授权新终端", used, seats)
			}
			writeError(w, http.StatusConflict, msg)
			return
		}
		if err := a.nodes.SetAuthorized(id, true); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n, err = a.nodes.Get(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
}

func (a *API) revokeNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.nodes.SetAuthorized(id, false); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := a.nodes.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
}

func (a *API) tokenStatus(w http.ResponseWriter, r *http.Request) {
	used, exp, nid, err := a.nodes.TokenStatus(r.PathValue("token"))
	if err != nil {
		writeError(w, http.StatusNotFound, "注册令牌不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"used": used, "expires_at": exp, "node_id": nid})
}
