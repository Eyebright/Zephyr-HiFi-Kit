package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"hifihub/server/internal/library"
)

const favoriteBodyLimit = 16 << 10

func favoriteAuthority(host, scheme string) (string, bool) {
	if (scheme != "http" && scheme != "https") || host == "" || strings.ContainsAny(host, "/\\?#@%, \t\r\n") || strings.HasSuffix(host, ":") {
		return "", false
	}
	authority, ok := settingsAuthority(host, scheme)
	if !ok {
		return "", false
	}
	name, port, err := net.SplitHostPort(authority)
	if err != nil || name == "" {
		return "", false
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	if strings.HasPrefix(host, "[") {
		if !strings.Contains(name, ":") || net.ParseIP(name) == nil {
			return "", false
		}
	} else {
		if strings.Count(host, ":") > 1 {
			return "", false
		}
		for _, c := range name {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_') {
				return "", false
			}
		}
	}
	return authority, true
}

func favoriteWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	// Favorites share playback's LAN access, not settings' privileged secure-context policy.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, ok := favoriteAuthority(r.Host, scheme)
	origins := r.Header.Values("Origin")
	if !ok || len(origins) != 1 || origins[0] == "" {
		writeErrorCode(w, http.StatusForbidden, "same_origin_required", "需要同源 Origin")
		return false
	}
	sites := r.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 || len(sites) == 1 && sites[0] != "same-origin" && sites[0] != "none" {
		writeErrorCode(w, http.StatusForbidden, "cross_site_forbidden", "不允许跨站请求")
		return false
	}
	u, err := url.Parse(origins[0])
	if err != nil || u.Scheme != scheme || u.User != nil || origins[0] != u.Scheme+"://"+u.Host {
		writeErrorCode(w, http.StatusForbidden, "cross_origin_forbidden", "不允许跨源请求")
		return false
	}
	origin, valid := favoriteAuthority(u.Host, u.Scheme)
	if !valid || origin != host {
		writeErrorCode(w, http.StatusForbidden, "cross_origin_forbidden", "不允许跨源请求")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" {
		writeErrorCode(w, http.StatusUnsupportedMediaType, "json_content_type_required", "需要 application/json")
		return false
	}
	return true
}

func favoriteQueryValues(r *http.Request, allowed ...string) (url.Values, error) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, invalidInput("query_invalid", "无效查询参数")
	}
	for key, values := range v {
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !known || len(values) != 1 {
			return nil, invalidInput("query_unknown_or_duplicate", fmt.Sprintf("未知或重复查询参数：%s", key), map[string]any{"field": key})
		}
	}
	return v, nil
}

func (a *API) listFavorites(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	v, err := favoriteQueryValues(r, "kind", "page", "page_size", "q")
	if err != nil {
		fail(w, err)
		return
	}
	q := library.FavoriteQuery{Kind: v.Get("kind"), Q: v.Get("q")}
	if v.Has("kind") && q.Kind == "" {
		writeErrorCode(w, http.StatusBadRequest, "favorite_kind_invalid", "kind 必须为 track 或 album")
		return
	}
	for key, dst := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v.Has(key) {
			n, err := strconv.Atoi(v.Get(key))
			if err != nil || n <= 0 {
				writeErrorCode(w, http.StatusBadRequest, "positive_integer_required", key+" 必须为正整数", map[string]any{"field": key})
				return
			}
			*dst = n
		}
	}
	page, err := a.lib.ListFavorites(q)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) favoriteState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	v, err := favoriteQueryValues(r, "track_id")
	if err != nil {
		fail(w, err)
		return
	}
	state, err := a.lib.GetFavoriteState(v.Get("track_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (a *API) saveFavorite(w http.ResponseWriter, r *http.Request) {
	a.writeFavorite(w, r, true)
}

func (a *API) deleteFavorite(w http.ResponseWriter, r *http.Request) {
	a.writeFavorite(w, r, false)
}

func (a *API) writeFavorite(w http.ResponseWriter, r *http.Request, save bool) {
	w.Header().Set("Cache-Control", "no-store")
	if a.lib == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "library_unavailable", "媒体库未启用")
		return
	}
	if !favoriteWriteAllowed(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, favoriteBodyLimit)
	defer r.Body.Close()
	var req library.FavoriteRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(&req)
	if err == nil {
		var extra any
		err = d.Decode(&extra)
		if err == io.EOF {
			err = nil
		} else if err == nil {
			err = fmt.Errorf("只允许一个 JSON 对象")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErrorCode(w, http.StatusRequestEntityTooLarge, "favorite_request_too_large", "收藏请求不能超过16KiB")
		} else {
			writeErrorCode(w, http.StatusBadRequest, "favorite_json_invalid", "无效收藏 JSON")
		}
		return
	}
	if save {
		err = a.lib.SaveFavorite(req)
	} else {
		err = a.lib.DeleteFavorite(req)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": save})
}
