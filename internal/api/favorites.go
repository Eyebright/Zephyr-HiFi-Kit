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
		writeError(w, http.StatusForbidden, "需要同源 Origin")
		return false
	}
	sites := r.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 || len(sites) == 1 && sites[0] != "same-origin" && sites[0] != "none" {
		writeError(w, http.StatusForbidden, "不允许跨站请求")
		return false
	}
	u, err := url.Parse(origins[0])
	if err != nil || u.Scheme != scheme || u.User != nil || origins[0] != u.Scheme+"://"+u.Host {
		writeError(w, http.StatusForbidden, "不允许跨源请求")
		return false
	}
	origin, valid := favoriteAuthority(u.Host, u.Scheme)
	if !valid || origin != host {
		writeError(w, http.StatusForbidden, "不允许跨源请求")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "需要 application/json")
		return false
	}
	return true
}

func favoriteQueryValues(r *http.Request, allowed ...string) (url.Values, error) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("无效查询参数")
	}
	for key, values := range v {
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !known || len(values) != 1 {
			return nil, fmt.Errorf("未知或重复查询参数：%s", key)
		}
	}
	return v, nil
}

func (a *API) listFavorites(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.lib == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
		return
	}
	v, err := favoriteQueryValues(r, "kind", "page", "page_size", "q")
	if err != nil {
		fail(w, err)
		return
	}
	q := library.FavoriteQuery{Kind: v.Get("kind"), Q: v.Get("q")}
	if v.Has("kind") && q.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind 必须为 track 或 album")
		return
	}
	for key, dst := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v.Has(key) {
			n, err := strconv.Atoi(v.Get(key))
			if err != nil || n <= 0 {
				writeError(w, http.StatusBadRequest, key+" 必须为正整数")
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
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
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
		writeError(w, http.StatusServiceUnavailable, "媒体库未启用")
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
			writeError(w, http.StatusRequestEntityTooLarge, "收藏请求不能超过16KiB")
		} else {
			writeError(w, http.StatusBadRequest, "无效收藏 JSON")
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
