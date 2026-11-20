package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
)

func (a *API) listBoms(w http.ResponseWriter, r *http.Request) {
	var bs []domain.Bom
	var err error
	if r.URL.Query().Get("status") == "published" {
		bs, err = a.boms.ListPublished()
	} else {
		bs, err = a.boms.List()
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (a *API) createBom(w http.ResponseWriter, r *http.Request) {
	var b domain.Bom
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	b, err := a.boms.Create(b)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (a *API) latestBom(w http.ResponseWriter, r *http.Request) {
	b, err := a.boms.Latest(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (a *API) listBomVersions(w http.ResponseWriter, r *http.Request) {
	bs, err := a.boms.Versions(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (a *API) getBom(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.Atoi(r.PathValue("ver"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_version", "version must be an integer", "")
		return
	}
	b, err := a.boms.Get(r.PathValue("id"), ver)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (a *API) validateBom(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.Atoi(r.PathValue("ver"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_version", "version must be an integer", "")
		return
	}
	b, err := a.boms.Get(r.PathValue("id"), ver)
	if err != nil {
		fail(w, err)
		return
	}
	rep := a.boms.Validate(b, r.URL.Query().Get("arch"))
	writeJSON(w, http.StatusOK, rep)
}

func (a *API) publishBom(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.Atoi(r.PathValue("ver"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_version", "version must be an integer", "")
		return
	}
	rep, err := a.boms.Publish(r.PathValue("id"), ver, r.URL.Query().Get("arch"))
	if err != nil {
		body := newAPIError(serviceErrorCode(err, "bom_publish_failed"), err.Error(), "")
		body.Error = &body.Message
		writeJSON(w, http.StatusConflict, struct {
			apiError
			Report any `json:"report"`
		}{body, rep})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"published": true, "report": rep})
}

// importBom accepts a bare BOM document or {"bom": {...}} and stores it as a
// new draft version under the same bom_id.
func (a *API) importBom(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "request_body_unreadable", "读取请求体失败")
		return
	}
	var b domain.Bom
	if err := json.Unmarshal(raw, &b); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if b.BomID == "" {
		var wrap struct {
			Bom domain.Bom `json:"bom"`
		}
		if err := json.Unmarshal(raw, &wrap); err == nil {
			b = wrap.Bom
		}
	}
	if b.BomID == "" {
		writeErrorCode(w, http.StatusBadRequest, "bom_id_required", "bom_id 不能为空")
		return
	}
	b, err = a.boms.Import(b)
	if err != nil {
		writeServiceError(w, http.StatusBadRequest, err, "bom_import_failed")
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// exportBom returns one BOM version as a JSON attachment.
func (a *API) exportBom(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.Atoi(r.PathValue("ver"))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_version", "version 必须是整数")
		return
	}
	b, err := a.boms.Get(r.PathValue("id"), ver)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, "bom_version_not_found", "BOM 版本不存在")
		return
	}
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+safeFilename(b.BomID)+`-v`+strconv.Itoa(b.Version)+`.json"`)
	writeJSON(w, http.StatusOK, b)
}

// rollbackBom copies an existing version into a new PUBLISHED version.
func (a *API) rollbackBom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version int `json:"version"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if body.Version <= 0 {
		writeErrorCode(w, http.StatusBadRequest, "positive_integer_required", "version 必须为正整数", map[string]any{"field": "version"})
		return
	}
	b, err := a.boms.Rollback(r.PathValue("id"), body.Version)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "bom_version_not_found", "BOM 版本不存在")
			return
		}
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// safeFilename keeps only filename-safe characters for Content-Disposition.
func safeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
}
