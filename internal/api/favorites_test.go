package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hifihub/server/internal/library"
	"hifihub/server/internal/store"
)

const favoriteTestOrigin = "http://192.168.10.2:8080"

func favoriteTestAPI(t *testing.T) (*API, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "favorites.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &API{lib: library.New(st, nil, ""), mux: http.NewServeMux(), admin: &settingsAdmin{configured: true}}
	a.route("GET /api/v1/favorites", a.listFavorites)
	a.route("GET /api/v1/favorites/state", a.favoriteState)
	a.route("PUT /api/v1/favorites", a.saveFavorite)
	a.route("DELETE /api/v1/favorites", a.deleteFavorite)
	if _, err := st.DB.Exec(`INSERT INTO mounts(id,host,share,target,created_at) VALUES('m','nas','music','/unused',1);
		INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album_artist,album,cover_hash,duration_ms,decodable,scanned_at) VALUES
		('t1','m','/unused/1','1','100%_live\set','Guest','Band','Shared','cover1',60000,1,1),
		('t2','m','/unused/2','2','Second','Band','','Shared','cover2',90000,1,1),
		('unknown','m','/unused/3','3','Unknown','','','','',0,0,1)`); err != nil {
		t.Fatal(err)
	}
	return a, st
}

func favoriteTestRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, favoriteTestOrigin+"/api/v1"+path, strings.NewReader(body))
	r.RemoteAddr = "192.168.10.3:54321"
	if method == http.MethodPut || method == http.MethodDelete {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", favoriteTestOrigin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	return r
}

func favoriteTestCall(t *testing.T, a *API, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, favoriteTestRequest(method, path, body))
	if w.Code != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("favorite response is cacheable: %v", w.Header())
	}
	return w
}

func favoriteTestPage(t *testing.T, a *API, query string) library.FavoritePage {
	t.Helper()
	w := favoriteTestCall(t, a, http.MethodGet, "/favorites"+query, "", http.StatusOK)
	var p library.FavoritePage
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFavoriteAPIContractAndMissingDelete(t *testing.T) {
	a, st := favoriteTestAPI(t)
	empty := favoriteTestPage(t, a, "")
	if empty.Items == nil || empty.Total != 0 || empty.Page != 1 || empty.PageSize != 24 {
		t.Fatalf("empty contract: %+v", empty)
	}
	for _, body := range []string{`{"kind":"track","track_id":"t1"}`, `{"kind":"album","track_id":"t1"}`} {
		w := favoriteTestCall(t, a, "PUT", "/favorites", body, http.StatusOK)
		if got := mustJSON(t, w.Body.Bytes()); !reflect.DeepEqual(got, map[string]any{"saved": true}) {
			t.Fatalf("save contract: %v", got)
		}
	}
	w := favoriteTestCall(t, a, "GET", "/favorites/state?track_id=t1", "", http.StatusOK)
	if got := mustJSON(t, w.Body.Bytes()); !reflect.DeepEqual(got, map[string]any{"track": true, "album": true, "artist": "Band", "album_name": "Shared"}) {
		t.Fatalf("state contract: %v", got)
	}
	track := favoriteTestPage(t, a, "?kind=track").Items[0]
	if track.Kind != "track" || track.Artist != "Guest" || track.TrackID != "t1" || track.Title != `100%_live\set` || track.CoverHash != "cover1" || track.DurationMS != 60000 || track.Missing || !track.Decodable || track.TrackCount != 1 || track.AddedAt == 0 {
		t.Fatalf("track contract: %+v", track)
	}
	album := favoriteTestPage(t, a, "?kind=album").Items[0]
	if album.Kind != "album" || album.Artist != "Band" || album.Album != "Shared" || album.Title != "Shared" || album.TrackID != "" || album.TrackCount != 2 || album.Missing || !album.Decodable {
		t.Fatalf("album contract: %+v", album)
	}
	// Explicit identity and either track resolve to one album without reordering.
	for _, body := range []string{`{"kind":"album","artist":"Band","album":"Shared"}`, `{"kind":"album","track_id":"t2"}`} {
		favoriteTestCall(t, a, "PUT", "/favorites", body, http.StatusOK)
	}
	if p := favoriteTestPage(t, a, "?kind=album"); p.Total != 1 || !reflect.DeepEqual(p.Items[0], album) {
		t.Fatalf("duplicate album PUT: %+v", p)
	}
	for _, term := range []string{"%", "_", `\`, "%_", "Guest", "Shared"} {
		if p := favoriteTestPage(t, a, "?kind=track&q="+url.QueryEscape(term)); p.Total != 1 {
			t.Fatalf("literal search %q: %+v", term, p)
		}
	}
	if p := favoriteTestPage(t, a, "?q="+url.QueryEscape("%' OR 1=1 --")); p.Total != 0 {
		t.Fatalf("search SQL injection: %+v", p)
	}
	if _, err := st.DB.Exec(`DELETE FROM tracks WHERE id IN ('t1','t2')`); err != nil {
		t.Fatal(err)
	}
	favoriteTestCall(t, a, "GET", "/favorites/state?track_id=t1", "", http.StatusNotFound)
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"album","track_id":"t1"}`, http.StatusNotFound)
	for _, kind := range []string{"track", "album"} {
		p := favoriteTestPage(t, a, "?kind="+kind)
		if p.Total != 1 || !p.Items[0].Missing || p.Items[0].Decodable || p.Items[0].TrackCount != 0 || p.Items[0].Title == "" || p.Items[0].CoverHash == "" {
			t.Fatalf("missing contract %s: %+v", kind, p)
		}
	}
	for _, body := range []string{`{"kind":"track","track_id":"t1"}`, `{"kind":"album","artist":"Band","album":"Shared"}`} {
		for i := 0; i < 2; i++ {
			w := favoriteTestCall(t, a, "DELETE", "/favorites", body, http.StatusOK)
			if got := mustJSON(t, w.Body.Bytes()); !reflect.DeepEqual(got, map[string]any{"saved": false}) {
				t.Fatalf("delete contract: %v", got)
			}
		}
	}
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"track","track_id":"absent"}`, http.StatusNotFound)
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"album","artist":"Missing","album":"Shared"}`, http.StatusNotFound)
	favoriteTestCall(t, a, "GET", "/favorites/state?track_id=absent", "", http.StatusNotFound)
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"album","artist":"","album":""}`, http.StatusOK)
	w = favoriteTestCall(t, a, "GET", "/favorites/state?track_id=unknown", "", http.StatusOK)
	if got := mustJSON(t, w.Body.Bytes()); got["album"] != true || got["artist"] != "" || got["album_name"] != "" {
		t.Fatalf("unknown album contract: %v", got)
	}
}

func TestFavoriteAPIPageValidationAndIdempotentOrder(t *testing.T) {
	a, st := favoriteTestAPI(t)
	for i, id := range []string{"t1", "t2"} {
		favoriteTestCall(t, a, "PUT", "/favorites", fmt.Sprintf(`{"kind":"track","track_id":%q}`, id), http.StatusOK)
		if _, err := st.DB.Exec(`UPDATE favorites SET added_at=? WHERE track_id=?`, i+1, id); err != nil {
			t.Fatal(err)
		}
	}
	before := favoriteTestPage(t, a, "")
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"track","track_id":"t1"}`, http.StatusOK)
	if after := favoriteTestPage(t, a, ""); !reflect.DeepEqual(before, after) || after.Items[0].TrackID != "t2" {
		t.Fatalf("duplicate reordered: before=%+v after=%+v", before, after)
	}
	for _, size := range []int{6, 24, 48, 96} {
		p := favoriteTestPage(t, a, fmt.Sprintf("?page=2&page_size=%d", size))
		if p.Page != 2 || p.PageSize != size || p.Total != 2 || p.Items == nil || len(p.Items) != 0 {
			t.Fatalf("page contract: %+v", p)
		}
	}
	for _, query := range []string{
		"kind=", "kind=artist", "kind=track&kind=album", "page=0", "page=-1", "page=", "page=1.5", "page=abc",
		"page=999999999999999999999999", "page=9223372036854775807", "page_size=0", "page_size=-1", "page_size=7", "page_size=25",
		"page_size=", "page_size=abc", "page_size=24&page_size=24", "page=1&page=2", "q=a&q=b", "q=%00", "q=%FF", "q=%ZZ", "q=x;y=z", "unexpected=1",
		"q=" + strings.Repeat("a", 1025),
	} {
		favoriteTestCall(t, a, "GET", "/favorites?"+query, "", http.StatusBadRequest)
	}
	for _, query := range []string{"", "track_id=", "track_id=%20", "track_id=%00", "track_id=t1&track_id=t2", "track_id=%ZZ", "track_id=t1&artist=Guest", "track_id=" + strings.Repeat("x", 513)} {
		favoriteTestCall(t, a, "GET", "/favorites/state?"+query, "", http.StatusBadRequest)
	}
}

func TestFavoriteAPIWriteSecurity(t *testing.T) {
	a, st := favoriteTestAPI(t)
	favoriteTestCall(t, a, "PUT", "/favorites", `{"kind":"track","track_id":"t1"}`, http.StatusOK)
	tests := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"no origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example:8080") }, 403},
		{"origin credentials", func(r *http.Request) { r.Header.Set("Origin", "http://user@192.168.10.2:8080") }, 403},
		{"scheme mismatch", func(r *http.Request) { r.Header.Set("Origin", "https://192.168.10.2:8080") }, 403},
		{"port mismatch", func(r *http.Request) { r.Header.Set("Origin", "http://192.168.10.2:8081") }, 403},
		{"origin path", func(r *http.Request) { r.Header.Set("Origin", favoriteTestOrigin+"/") }, 403},
		{"origin empty query", func(r *http.Request) { r.Header.Set("Origin", favoriteTestOrigin+"?") }, 403},
		{"origin empty fragment", func(r *http.Request) { r.Header.Set("Origin", favoriteTestOrigin+"#") }, 403},
		{"multiple origins", func(r *http.Request) { r.Header.Add("Origin", favoriteTestOrigin) }, 403},
		{"merged origins", func(r *http.Request) { r.Header.Set("Origin", favoriteTestOrigin+", "+favoriteTestOrigin) }, 403},
		{"cross site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"same site is not same origin", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
		{"invalid fetch site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "garbage") }, 403},
		{"empty fetch site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "") }, 403},
		{"multiple fetch sites", func(r *http.Request) { r.Header.Add("Sec-Fetch-Site", "cross-site") }, 403},
		{"untrusted forwarded host", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.example")
			r.Header.Set("Forwarded", "host=evil.example;proto=https")
			r.Header.Set("X-Forwarded-Host", "evil.example")
			r.Header.Set("X-Forwarded-Proto", "https")
		}, 403},
		{"invalid host port", func(r *http.Request) {
			r.Host = "192.168.10.2:65536"
			r.Header.Set("Origin", "http://"+r.Host)
		}, 403},
		{"invalid IPv6 authority", func(r *http.Request) {
			r.Host = "[not-an-ip]:8080"
			r.Header.Set("Origin", "http://"+r.Host)
		}, 403},
		{"no JSON content type", func(r *http.Request) { r.Header.Del("Content-Type") }, 415},
		{"simple form content type", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415},
		{"text content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"multiple content types", func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }, 415},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{"PUT", "DELETE"} {
				id := "t2" // Unsafe PUT must not insert; unsafe DELETE must not remove.
				if method == "DELETE" {
					id = "t1"
				}
				r := favoriteTestRequest(method, "/favorites", fmt.Sprintf(`{"kind":"track","track_id":%q}`, id))
				tc.change(r)
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("%s: got %d want %d: %s", method, w.Code, tc.status, w.Body.String())
				}
			}
			var count int
			if err := st.DB.QueryRow(`SELECT COUNT(*) FROM favorites WHERE kind='track' AND track_id='t1'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("unsafe deletion: %d %v", count, err)
			}
			if err := st.DB.QueryRow(`SELECT COUNT(*) FROM favorites`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("unsafe insertion: %d %v", count, err)
			}
		})
	}
}

func TestFavoriteAPIAllowsLANHTTPAndTLSWithoutAdmin(t *testing.T) {
	a, _ := favoriteTestAPI(t)
	for _, tc := range []struct {
		host, origin, site string
		tls                bool
	}{
		{"192.168.10.2:8080", favoriteTestOrigin, "same-origin", false},
		{"music.lan", "http://MUSIC.LAN:80", "", false},
		{"music.lan:80", "http://music.lan", "none", false},
		{"[fd00::1]:8080", "http://[fd00::1]:8080", "same-origin", false},
		{"[::1]", "http://[::1]:80", "same-origin", false},
		{"music.lan:443", "https://music.lan", "same-origin", true},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			for _, method := range []string{"PUT", "DELETE"} {
				r := favoriteTestRequest(method, "/favorites", `{"kind":"track","track_id":"t1"}`)
				r.Host = tc.host
				r.Header.Set("Origin", tc.origin)
				r.Header.Set("Content-Type", "application/json; charset=utf-8")
				if tc.site == "" {
					r.Header.Del("Sec-Fetch-Site")
				} else {
					r.Header.Set("Sec-Fetch-Site", tc.site)
				}
				if tc.tls {
					r.TLS = &tls.ConnectionState{}
				}
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				if w.Code != http.StatusOK {
					t.Fatalf("ordinary favorite needs no admin login: %s %d %s", method, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestFavoriteAuthorityRejectsMalformedAuthorities(t *testing.T) {
	for _, host := range []string{"", "music.lan:", "music.lan:0", "music.lan:65536", "music.lan:no", "music.lan:-1", "music.lan/path", "music.lan?", "music.lan#", "music.lan\\evil", "user@music.lan", " music.lan", "music.lan\n", "[garbage]", "[127.0.0.1]", "::1", "[::1", "[::1]:", "[::1]:65536", "music%2elan"} {
		if authority, ok := favoriteAuthority(host, "http"); ok {
			t.Fatalf("malformed authority accepted: %q -> %q", host, authority)
		}
	}
	if _, ok := favoriteAuthority("music.lan", "ftp"); ok {
		t.Fatal("invalid scheme accepted")
	}
}

func TestFavoriteAPIJSONLimitsAndInvalidRequests(t *testing.T) {
	a, _ := favoriteTestAPI(t)
	for _, method := range []string{"PUT", "DELETE"} {
		for _, body := range []string{
			"", "null", "[]", `"text"`, "{", "{}", `{"kind":"artist","track_id":"t1"}`, `{"kind":"track"}`,
			`{"kind":"track","track_id":4}`, `{"kind":"track","track_id":null}`, `{"kind":"track","track_id":" "}`,
			`{"kind":"track","track_id":"t1","title":"forged"}`, `{"kind":"track","track_id":"t1","artist":"Guest"}`,
			`{"kind":"album","artist":"Band"}`, `{"kind":"album","album":"Shared"}`, `{"kind":"album","artist":null,"album":"Shared"}`,
			`{"kind":"album","track_id":"t1","artist":"Band","album":"Shared"}`,
			`{"kind":"track","track_id":"t1"} {}`, `{"kind":"track","track_id":"t1"} garbage`,
			`{"kind":"track","track_id":"\u0000"}`, `{"kind":"track","track_id":"` + strings.Repeat("x", 513) + `"}`,
		} {
			favoriteTestCall(t, a, method, "/favorites", body, http.StatusBadRequest)
		}
		favoriteTestCall(t, a, method, "/favorites", `{"kind":"track","track_id":"`+strings.Repeat("x", favoriteBodyLimit)+`"}`, http.StatusRequestEntityTooLarge)
		// A valid prefix must not bypass the limit with unread trailing bytes.
		favoriteTestCall(t, a, method, "/favorites", `{"kind":"track","track_id":"t1"}`+strings.Repeat(" ", favoriteBodyLimit), http.StatusRequestEntityTooLarge)
	}
	favoriteTestCall(t, a, "DELETE", "/favorites", `{"kind":"album","track_id":"t1"}`, http.StatusBadRequest)
	if p := favoriteTestPage(t, a, ""); p.Total != 0 {
		t.Fatalf("invalid request wrote favorites: %+v", p)
	}
}

func TestFavoriteAPILibraryDisabled(t *testing.T) {
	a := &API{}
	for _, h := range []http.HandlerFunc{a.listFavorites, a.favoriteState, a.saveFavorite, a.deleteFavorite} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("disabled library: %d %s", w.Code, w.Body.String())
		}
	}
}
