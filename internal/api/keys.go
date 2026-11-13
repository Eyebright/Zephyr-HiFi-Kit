package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
)

func (a *API) listKeys(w http.ResponseWriter, _ *http.Request) {
	ks, err := a.keys.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ks)
}

func (a *API) importKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Armored   string `json:"armored"`
		PublicKey string `json:"public_key"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	material := body.Armored
	if material == "" {
		material = body.PublicKey
	}
	imported, err := a.keys.ImportPublic(material)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

func (a *API) setTrustRoot(w http.ResponseWriter, r *http.Request) {
	k, err := a.keys.SetTrustRoot(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "密钥不存在："+r.PathValue("id"))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (a *API) exportTrustRoot(w http.ResponseWriter, _ *http.Request) {
	armor, err := a.keys.ExportTrustRootArmor()
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "尚未设置信任根密钥")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pgp-keys; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="trust-root.asc"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(armor)
}
