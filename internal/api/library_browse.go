package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"hifihub/server/internal/library"
)

func parseBrowseQuery(v url.Values) (library.BrowseQuery, error) {
	q := library.BrowseQuery{Category: v.Get("category"), Q: v.Get("q"), Field: v.Get("field"), Sort: v.Get("sort"), Direction: v.Get("direction"), ArtistMode: v.Get("artist_mode"), Initial: v.Get("initial"), MountID: v.Get("mount_id"), Directory: v.Get("directory")}
	for key, values := range v {
		if len(values) != 1 {
			return q, invalidInput("query_duplicate", fmt.Sprintf("重复的查询参数：%s", key), map[string]any{"field": key})
		}
	}
	for key, dst := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v.Has(key) {
			n, err := strconv.Atoi(v.Get(key))
			if err != nil || n <= 0 {
				return q, invalidInput("positive_integer_required", fmt.Sprintf("%s 必须为正整数", key), map[string]any{"field": key})
			}
			*dst = n
		}
	}
	if v.Has("offset") {
		n, err := strconv.Atoi(v.Get("offset"))
		if err != nil || n < 0 {
			return q, invalidInput("positive_integer_required", "offset 必须为非负整数", map[string]any{"field": "offset"})
		}
		q.Offset = n
	}
	for key, dst := range map[string]**string{"artist": &q.Artist, "album": &q.Album, "genre": &q.Genre, "composer": &q.Composer} {
		if v.Has(key) {
			s := v.Get(key)
			*dst = &s
		}
	}
	if v.Has("year") {
		n, err := strconv.Atoi(v.Get("year"))
		if err != nil || n < 0 || n > 9999 {
			return q, invalidInput("year_invalid", "year 必须为 0..9999（0 表示未知）")
		}
		q.Year = &n
	}
	for key, dst := range map[string]**int{"year_from": &q.YearFrom, "year_to": &q.YearTo} {
		if v.Has(key) {
			n, err := strconv.Atoi(v.Get(key))
			if err != nil || n < 1 || n > 9999 {
				return q, invalidInput("year_range_invalid", fmt.Sprintf("%s 必须为 1..9999", key), map[string]any{"field": key})
			}
			*dst = &n
		}
	}
	// Pairing, ordering and exclusivity are validated by both service methods.
	if v.Has("recursive") {
		b, err := strconv.ParseBool(v.Get("recursive"))
		if err != nil {
			return q, invalidInput("recursive_invalid", "recursive 必须为布尔值")
		}
		q.Recursive = b
	}
	return q, nil
}

func (a *API) withLibraryMaintenance(next http.HandlerFunc) http.HandlerFunc {
	return a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		if a.zat == nil {
			next(w, r)
			return
		}
		a.zat.WithLibraryMaintenance(func() { next(w, r) })
	})
}

func (a *API) browseLibrary(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	q, err := parseBrowseQuery(r.URL.Query())
	if err != nil {
		fail(w, err)
		return
	}
	page, err := a.lib.Browse(q)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) artistIndex(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	mode := r.URL.Query().Get("artist_mode")
	if mode == "" {
		mode = r.URL.Query().Get("mode")
	}
	idx, err := a.lib.ArtistIndex(r.Context(), mode)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, idx)
}

func (a *API) selectLibraryTracks(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	q, err := parseBrowseQuery(r.URL.Query())
	if err != nil {
		fail(w, err)
		return
	}
	ids, err := a.lib.SelectTracks(q)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"track_ids": ids, "total": len(ids)})
}

func (a *API) patchMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	var req library.AddMountRequest
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	m, err := a.lib.UpdateMount(r.PathValue("id"), req)
	if err != nil {
		fail(w, err, "source_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) testMount(w http.ResponseWriter, r *http.Request) {
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	m, err := a.lib.TestMount(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err, "source_test_failed")
		return
	}
	writeJSON(w, http.StatusOK, m)
}
