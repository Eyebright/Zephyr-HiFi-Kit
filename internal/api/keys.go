package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
)

func (a *API) listKeys(w http.ResponseWriter, _ *http.Request) {
	ks, err := a.keys.List()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
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
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	material := body.Armored
	if material == "" {
		material = body.PublicKey
	}
	imported, err := a.keys.ImportPublic(material)
	if err != nil {
		writeServiceError(w, http.StatusBadRequest, err, "key_import_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

func (a *API) setTrustRoot(w http.ResponseWriter, r *http.Request) {
	k, err := a.keys.SetTrustRoot(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "key_not_found", "密钥不存在："+r.PathValue("id"), map[string]any{"id": r.PathValue("id")})
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (a *API) exportTrustRoot(w http.ResponseWriter, _ *http.Request) {
	armor, err := a.keys.ExportTrustRootArmor()
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "trust_root_missing", "尚未设置信任根密钥")
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/pgp-keys; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="trust-root.asc"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(armor)
}
