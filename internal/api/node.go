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
		TTLSeconds int    `json:"ttl_seconds"`
		Digits     int    `json:"digits"`
		Arch       string `json:"arch"`
	}
	_ = decode(r, &body)
	ttl := 30 * time.Minute
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	var token string
	var exp int64
	var err error
	if body.Digits > 0 || body.Arch == "android" {
		digits := body.Digits
		if digits == 0 {
			digits = 8
		}
		token, exp, err = a.nodes.IssueNumericToken(ttl, digits)
	} else {
		token, exp, err = a.nodes.IssueToken(ttl)
	}
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
	// Android terminals pair with a short numeric code and have no operator
	// console step, so registration grants a seat automatically while seats
	// remain. Seat-full nodes stay unauthorized and can be granted manually.
	if req.Arch == "android" {
		a.seatMu.Lock()
		if seats, _, lerr := a.lic.SeatLimit(); lerr == nil {
			if used, uerr := a.nodes.AuthorizedCount(); uerr == nil && used < seats {
				if serr := a.nodes.SetAuthorized(n.NodeID, true); serr == nil {
					n.Authorized = true
				}
			}
		}
		a.seatMu.Unlock()
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
	case "devices":
		a.nodeDevices(w, r)
	case "queue":
		a.nodeQueue(w, r)
	default:
		writeErrorCode(w, http.StatusNotFound, "node_resource_not_found", "未知的终端子资源")
	}
}

func (a *API) patchNode(w http.ResponseWriter, r *http.Request) {
	var p node.Patch
	if err := decode(r, &p); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
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
			writeErrorCode(w, http.StatusBadRequest, "bom_not_found", "BOM 不存在："+*p.BomID, map[string]any{"id": *p.BomID})
			return
		}
		if b.Status != domain.BomPublished {
			writeErrorCode(w, http.StatusConflict, "bom_unpublished", "BOM 未发布，不能绑定到终端")
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
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeServiceError(w, http.StatusBadRequest, err, "node_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request) {
	n, err := a.nodes.Delete(r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, node.ErrPlaying):
			writeServiceError(w, http.StatusConflict, err)
		case errors.Is(err, catalog.ErrNotFound):
			writeServiceError(w, http.StatusNotFound, err)
		default:
			writeServiceError(w, http.StatusInternalServerError, err)
		}
		return
	}
	if a.rolls != nil {
		if err := a.rolls.StripNode(n.NodeID); err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
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
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if body.Node.NodeID == "" {
		writeErrorCode(w, http.StatusBadRequest, "node_id_required", "node_id 不能为空")
		return
	}
	a.seatMu.Lock()
	defer a.seatMu.Unlock()
	if body.Node.Authorized {
		seats, _, err := a.lic.SeatLimit()
		if err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
			return
		}
		used, err := a.nodes.AuthorizedCount()
		if err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
			return
		}
		if used >= seats {
			writeErrorCode(w, http.StatusConflict, "restore_seats_full", "授权席位已满，无法恢复已授权终端")
			return
		}
	}
	n, err := a.nodes.Restore(body.Node)
	if err != nil {
		writeServiceError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": n})
}

func (a *API) authorizeNode(w http.ResponseWriter, r *http.Request) {
	a.seatMu.Lock()
	defer a.seatMu.Unlock()
	seats, licensed, err := a.lic.SeatLimit()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	id := r.PathValue("id")
	n, err := a.nodes.Get(id)
	if err != nil {
		writeServiceError(w, http.StatusNotFound, err)
		return
	}
	if !n.Authorized {
		used, err := a.nodes.AuthorizedCount()
		if err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
			return
		}
		if used >= seats {
			code := "builtin_seats_full"
			msg := fmt.Sprintf("内置授权席位已满（%d/%d），激活许可证可授权更多终端", used, seats)
			if licensed {
				code = "license_seats_full"
				msg = fmt.Sprintf("许可证席位已满（%d/%d），无法授权新终端", used, seats)
			}
			writeErrorCode(w, http.StatusConflict, code, msg, map[string]any{"used": used, "seats": seats})
			return
		}
		if err := a.nodes.SetAuthorized(id, true); err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
			return
		}
		if n, err = a.nodes.Get(id); err != nil {
			writeServiceError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
}

func (a *API) revokeNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.nodes.SetAuthorized(id, false); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	n, err := a.nodes.Get(id)
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
}

func (a *API) tokenStatus(w http.ResponseWriter, r *http.Request) {
	used, exp, nid, err := a.nodes.TokenStatus(r.PathValue("token"))
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, "registration_token_not_found", "注册令牌不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"used": used, "expires_at": exp, "node_id": nid})
}
