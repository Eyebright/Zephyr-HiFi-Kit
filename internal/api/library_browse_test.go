package api

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/server/internal/store"
)

func TestParseBrowseQueryUnknownAndValidation(t *testing.T) {
	q, err := parseBrowseQuery(url.Values{"category": {"artists"}, "artist": {""}, "year": {"0"}, "page_size": {"48"}, "offset": {"96"}})
	if err != nil || q.Artist == nil || *q.Artist != "" || q.Year == nil || *q.Year != 0 || q.PageSize != 48 || q.Offset != 96 {
		t.Fatalf("unknown filter: %+v %v", q, err)
	}
	q, err = parseBrowseQuery(url.Values{"category": {"albums"}, "artist": {"Band"}, "album": {"Shared"}, "year_from": {"1980"}, "year_to": {"1989"}})
	if err != nil || q.Year != nil || q.YearFrom == nil || *q.YearFrom != 1980 || q.YearTo == nil || *q.YearTo != 1989 || q.Artist == nil || *q.Artist != "Band" || q.Album == nil || *q.Album != "Shared" {
		t.Fatalf("range filter: %+v %v", q, err)
	}
	for _, v := range []url.Values{
		{"page": {"-1"}}, {"page": {"1", "2"}}, {"page_size": {"x"}}, {"offset": {"-1"}}, {"year": {"oops"}}, {"recursive": {"maybe"}},
		{"year_from": {""}}, {"year_to": {""}}, {"year_from": {"oops"}}, {"year_to": {"1989.5"}},
		{"year_from": {"0"}}, {"year_to": {"0"}}, {"year_from": {"-1"}}, {"year_to": {"-1"}},
		{"year_from": {"10000"}}, {"year_to": {"10000"}}, {"year_from": {"9999999999999999999999999999"}},
		{"year_from": {"1980", "1990"}}, {"year_to": {"1989", "1999"}},
	} {
		if _, err := parseBrowseQuery(v); err == nil {
			t.Fatalf("invalid accepted: %v", v)
		}
	}
}

func TestLibraryBrowseYearRangeValidation(t *testing.T) {
	base := setup(t, "dev")
	for _, endpoint := range []string{"browse", "select"} {
		for _, query := range []string{
			"year_from=1980", "year_to=1989", "year_from=&year_to=1989", "year_from=1980&year_to=",
			"year_from=bad&year_to=1989", "year_from=1980&year_to=1989.5",
			"year_from=0&year_to=9", "year_from=-1&year_to=9", "year_from=1&year_to=0", "year_from=1&year_to=-1",
			"year_from=9990&year_to=10000", "year_from=10000&year_to=10000", "year_from=1990&year_to=1989",
			"year_from=1980&year_to=1989&year=1985", "year_from=1&year_to=9&year=0",
			"year_from=1980&year_from=1990&year_to=1989", "year_from=1980&year_to=1989&year_to=1999",
			"year_from=1&year_to=9999999999999999999999999999",
		} {
			status, body := doJSON(t, "GET", base+"/library/"+endpoint+"?category=albums&"+query, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("%s accepted invalid %s: %d %s", endpoint, query, status, body)
			}
		}
		for _, query := range []string{"year_from=1&year_to=9", "year_from=9999&year_to=9999", "year_from=1&year_to=9999", "year=0", "year=1989"} {
			status, body := doJSON(t, "GET", base+"/library/"+endpoint+"?category=albums&"+query, nil)
			if status != http.StatusOK {
				t.Fatalf("%s rejected valid %s: %d %s", endpoint, query, status, body)
			}
		}
	}
}

func TestLibraryBrowseDecadeRangeContract(t *testing.T) {
	base, _, dir := setupFull(t, "dev")
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB.Exec(`INSERT INTO mounts(id,host,share,target,created_at,kind) VALUES('decades','localhost','Decades','/decades',1,'local')`); err != nil {
		t.Fatal(err)
	}
	years := []int{0, 1, 9, 1979, 1990, 9999}
	for i := 0; i < 27; i++ {
		years = append(years, 1989) // No 1980 or 1985 tracks: searches still find the decade.
	}
	for i, year := range years {
		id := fmt.Sprintf("t%02d", i)
		if _, err := st.DB.Exec(`INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album,year,scanned_at,decodable)
			VALUES(?,'decades',?,?,?,?,?,?,1,1)`, id, "/decades/"+id+".wav", id+".wav", id, "Band", "Album "+id, year); err != nil {
			t.Fatal(err)
		}
	}
	status, body := doJSON(t, "GET", base+"/library/browse?category=years", nil)
	if status != http.StatusOK {
		t.Fatalf("browse decades: %d %s", status, body)
	}
	page := mustJSON(t, body)
	if page["total"] != float64(6) || page["total_tracks"] != float64(33) {
		t.Fatalf("decade totals: %s", body)
	}
	items := page["items"].([]any)
	unknown := items[0].(map[string]any)
	if unknown["kind"] != "year" || unknown["name"] != "未知年份" || unknown["value"] != "0" || unknown["year"] != float64(0) || unknown["year_from"] != nil || unknown["year_to"] != nil {
		t.Fatalf("unknown contract: %+v", unknown)
	}
	decade := items[3].(map[string]any)
	if decade["kind"] != "year" || decade["name"] != "1980–1989" || decade["value"] != "1980–1989" || decade["year_from"] != float64(1980) || decade["year_to"] != float64(1989) || decade["album_count"] != float64(27) {
		t.Fatalf("decade contract: %+v", decade)
	}
	for _, term := range []string{"1980", "1985", "1980-1989", "1980–1989"} {
		query := url.Values{"category": {"years"}, "field": {"year"}, "q": {term}}
		status, body := doJSON(t, "GET", base+"/library/browse?"+query.Encode(), nil)
		if status != http.StatusOK || mustJSON(t, body)["total"] != float64(1) || mustJSON(t, body)["total_tracks"] != float64(27) {
			t.Fatalf("decade search %q: %d %s", term, status, body)
		}
		query.Set("page", "2")
		status, body = doJSON(t, "GET", base+"/library/select?"+query.Encode(), nil)
		if status != http.StatusOK || mustJSON(t, body)["total"] != float64(27) {
			t.Fatalf("decade search selection %q: %d %s", term, status, body)
		}
	}
	query := "category=albums&artist=Band&year_from=1980&year_to=1989&page=2&page_size=24"
	status, body = doJSON(t, "GET", base+"/library/browse?"+query, nil)
	if status != http.StatusOK {
		t.Fatalf("range drilldown: %d %s", status, body)
	}
	page = mustJSON(t, body)
	if page["total"] != float64(27) || page["total_tracks"] != float64(27) || len(page["items"].([]any)) != 3 {
		t.Fatalf("range drilldown pagination: %s", body)
	}
	status, body = doJSON(t, "GET", base+"/library/select?"+query, nil)
	if status != http.StatusOK || mustJSON(t, body)["total"] != float64(27) || len(mustJSON(t, body)["track_ids"].([]any)) != 27 {
		t.Fatalf("range selection across pages: %d %s", status, body)
	}
	for _, filter := range []string{"year=0", "year=1990", "year_from=9999&year_to=9999", "year_from=1980&year_to=1989&artist=Band&album=Album+t06"} {
		status, body := doJSON(t, "GET", base+"/library/select?category=albums&"+filter, nil)
		if status != http.StatusOK || mustJSON(t, body)["total"] != float64(1) {
			t.Fatalf("exact/range filters %s: %d %s", filter, status, body)
		}
	}
}

func TestLocalSourceSettingsContract(t *testing.T) {
	base := setup(t, "dev")
	status, body := doJSON(t, "POST", base+"/mounts", map[string]any{"kind": "local", "path": t.TempDir()})
	if status != 201 {
		t.Fatalf("add local source: %d %s", status, body)
	}
	id := mustJSON(t, body)["id"].(string)
	status, body = doJSON(t, "POST", base+"/mounts/"+id+"/test", nil)
	if status != 200 || mustJSON(t, body)["id"] != id {
		t.Fatalf("test source contract: %d %s", status, body)
	}
	status, body = doJSON(t, "PATCH", base+"/mounts/"+id, map[string]any{"kind": "local", "share": "音乐"})
	if status != 200 || mustJSON(t, body)["share"] != "音乐" {
		t.Fatalf("update source: %d %s", status, body)
	}
}

func TestLibraryBrowseRoutesAndManagementBoundary(t *testing.T) {
	base := setup(t, "dev")
	for _, category := range []string{"artists", "albums", "tracks", "genres", "years", "composers", "folders"} {
		status, body := doJSON(t, "GET", base+"/library/browse?category="+category+"&page_size=24", nil)
		if status != 200 {
			t.Fatalf("%s: %d %s", category, status, body)
		}
		p := mustJSON(t, body)
		if p["items"] == nil || p["total"].(float64) != 0 {
			t.Fatalf("empty page: %s", body)
		}
	}
	for _, path := range []string{"/mounts", "/mounts/test/scan", "/settings/backup", "/settings/restore", "/settings/cleanup", "/license/activate", "/apps", "/nodes/test/commands", "/nodes/restore"} {
		req, _ := http.NewRequest("POST", base+path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", req.URL.Scheme+"://"+req.URL.Host)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unguarded %s: %d", path, resp.StatusCode)
		}
	}
	status, body := doJSON(t, "GET", base+"/library/select?category=tracks", nil)
	if status != 200 || mustJSON(t, body)["total"].(float64) != 0 {
		t.Fatalf("select: %d %s", status, body)
	}
}
