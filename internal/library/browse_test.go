package library

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func browseString(s string) *string { return &s }
func browseYear(n int) *int         { return &n }

func seedBrowseTrack(t *testing.T, s *Service, tr Track) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album,genre,album_artist,composer,year,
		disc_number,track_number,cover_hash,duration_ms,added_at,scanned_at,decodable) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tr.ID, tr.MountID, "/test/"+tr.RelPath, tr.RelPath, tr.Title, tr.Artist, tr.Album, tr.Genre, tr.AlbumArtist, tr.Composer, tr.Year,
		tr.DiscNumber, tr.TrackNumber, tr.CoverHash, tr.DurationMS, tr.AddedAt, 1, boolInt(tr.Decodable))
	if err != nil {
		t.Fatal(err)
	}
}

func browseFixture(t *testing.T) (*Service, string) {
	t.Helper()
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	tracks := []Track{
		{ID: "a", Title: "花", Artist: "Guest", AlbumArtist: "王菲", Album: "同名", Genre: "Jazz; Rock; Jazz", Composer: "Bach, Johann Sebastian; Mozart", Year: 2001, RelPath: "中文/子目录/a.wav", Decodable: true, DurationMS: 1000, AddedAt: 10, CoverHash: "a-cover", DiscNumber: 1, TrackNumber: 2},
		{ID: "b", Title: "百", Artist: "王菲", Album: "同名", Genre: "Jazz", Composer: "Mozart", Year: 2002, RelPath: "b.wav", Decodable: true, DurationMS: 2000, AddedAt: 20},
		{ID: "c", Title: "陈曲", Artist: "陈奕迅", Album: "同名", Genre: "Pop", Composer: "陈", Year: 2000, RelPath: "c.wav", Decodable: true, DurationMS: 3000, AddedAt: 30},
		{ID: "d", Title: "100%_live", Artist: "ABC", RelPath: "literal.wav", DurationMS: 4000, AddedAt: 40},
		{ID: "e", Title: "Unknown", RelPath: "Other/e.wav", Decodable: true, DurationMS: 5000, AddedAt: 50},
	}
	for _, tr := range tracks {
		tr.MountID = m.ID
		seedBrowseTrack(t, s, tr)
	}
	return s, m.ID
}

func mustBrowse(t *testing.T, s *Service, q BrowseQuery) BrowsePage {
	t.Helper()
	page, err := s.Browse(q)
	if err != nil {
		t.Fatalf("Browse(%+v): %v", q, err)
	}
	return page
}

func TestBrowseSevenCategoriesAndPinyin(t *testing.T) {
	s, m := browseFixture(t)
	for category, total := range map[string]int{"artists": 4, "albums": 4, "tracks": 5, "genres": 4, "years": 2, "composers": 4, "folders": 1} {
		page := mustBrowse(t, s, BrowseQuery{Category: category})
		if page.Category != category || page.Total != total || page.TotalTracks != 5 || page.Page != 1 || page.PageSize != 20 || len(page.Items) != total {
			t.Fatalf("%s: %+v", category, page)
		}
	}
	artists := mustBrowse(t, s, BrowseQuery{Category: "artists"})
	var names []string
	for _, item := range artists.Items {
		names = append(names, item.Value)
	}
	if !reflect.DeepEqual(names, []string{"", "ABC", "陈奕迅", "王菲"}) {
		t.Fatalf("pinyin ordering: %v", names)
	}
	wang := mustBrowse(t, s, BrowseQuery{Category: "artists", Initial: "W"})
	if wang.Total != 1 || wang.Items[0].Value != "王菲" || wang.Items[0].TrackCount != 2 || wang.Items[0].AlbumCount != 1 || wang.Items[0].DurationMS != 3000 || wang.Items[0].CoverHash != "a-cover" {
		t.Fatalf("album artist grouping: %+v", wang)
	}
	guest := mustBrowse(t, s, BrowseQuery{Category: "artists", ArtistMode: "track", Initial: "G"})
	if guest.Total != 1 || guest.Items[0].Value != "Guest" || guest.TotalTracks != 1 {
		t.Fatalf("track artists: %+v", guest)
	}
	albums := mustBrowse(t, s, BrowseQuery{Category: "albums", Q: "同名"})
	if albums.Total != 2 || albums.TotalTracks != 3 {
		t.Fatalf("album identity: %+v", albums)
	}
	album := mustBrowse(t, s, BrowseQuery{Category: "albums", Artist: browseString("王菲"), Album: browseString("同名")})
	if album.Total != 1 || album.Items[0].Artist != "王菲" || album.Items[0].TrackCount != 2 {
		t.Fatalf("album filters: %+v", album)
	}
	track := mustBrowse(t, s, BrowseQuery{Category: "tracks", Q: "花"})
	if track.Items[0].Track == nil || track.Items[0].Track.AlbumArtist != "王菲" || track.Items[0].Track.Composer != "Bach, Johann Sebastian; Mozart" || track.Items[0].Track.TrackNumber != 2 {
		t.Fatalf("track metadata: %+v", track)
	}
	folders := mustBrowse(t, s, BrowseQuery{Category: "folders", MountID: m})
	if folders.Total != 5 || folders.TotalTracks != 5 {
		t.Fatalf("folders: %+v", folders)
	}
}

func TestBrowseLiteralFieldSearchAndUnknownEntities(t *testing.T) {
	s, _ := browseFixture(t)
	for _, tc := range []struct {
		q             BrowseQuery
		total, tracks int
	}{
		{BrowseQuery{Category: "genres", Q: "Jazz"}, 1, 2},
		{BrowseQuery{Category: "composers", Q: "Bach, Johann"}, 1, 1},
		{BrowseQuery{Category: "tracks", Q: "%_"}, 1, 1},
		{BrowseQuery{Category: "tracks", Q: "%' OR 1=1 --"}, 0, 0},
		{BrowseQuery{Category: "artists", Q: "花"}, 0, 0},
		{BrowseQuery{Category: "artists", Q: "花", Field: "title"}, 1, 1},
		{BrowseQuery{Category: "tracks", Q: "Guest", Field: "all"}, 1, 1},
		{BrowseQuery{Category: "tracks", Q: "王菲", Field: "artist"}, 2, 2},
		{BrowseQuery{Category: "tracks", Q: "子目录", Field: "path"}, 1, 1},
		{BrowseQuery{Category: "years", Q: "200"}, 1, 3},
		{BrowseQuery{Category: "tracks", Artist: browseString("")}, 1, 1},
		{BrowseQuery{Category: "tracks", Album: browseString(""), Genre: browseString(""), Composer: browseString(""), Year: browseYear(0)}, 2, 2},
		{BrowseQuery{Category: "tracks", Genre: browseString("Rock"), Composer: browseString("Bach, Johann Sebastian")}, 1, 1},
		{BrowseQuery{Category: "tracks", Initial: "0-9"}, 1, 1},
		{BrowseQuery{Category: "artists", Initial: "#"}, 1, 1},
		{BrowseQuery{Category: "folders", Q: "music"}, 1, 5},
		{BrowseQuery{Category: "folders", Field: "all", Q: "花"}, 1, 1},
	} {
		page := mustBrowse(t, s, tc.q)
		if page.Total != tc.total || page.TotalTracks != tc.tracks {
			t.Fatalf("query=%+v got=%+v want=%d/%d", tc.q, page, tc.total, tc.tracks)
		}
	}
	unknown := mustBrowse(t, s, BrowseQuery{Category: "years", Year: browseYear(0)})
	if unknown.Total != 1 || unknown.Items[0].Value != "0" || unknown.Items[0].Year != 0 || unknown.Items[0].Initial != "#" {
		t.Fatalf("unknown year: %+v", unknown)
	}
	empty := mustBrowse(t, s, BrowseQuery{Q: "never matches"})
	b, _ := json.Marshal(empty)
	if empty.Items == nil || !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("non-null page: %s", b)
	}
}

func decadeBrowseFixture(t *testing.T) *Service {
	t.Helper()
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	for _, tr := range []Track{
		{ID: "unknown", Year: 0},
		{ID: "first", Year: 1, Artist: "Early", Album: "Early"},
		{ID: "ninth", Year: 9, Artist: "Early", Album: "Early"},
		{ID: "nineteenth", Year: 19},
		{ID: "ninety-ninth", Year: 99},
		{ID: "hundredth", Year: 100},
		{ID: "before", Year: 1979},
		{ID: "start", Year: 1980, Artist: "Guest", AlbumArtist: "Band", Album: "Shared"},
		{ID: "end", Year: 1989, Artist: "Band", Album: "Shared"},
		{ID: "other", Year: 1989, Artist: "Solo", Album: "Shared"},
		{ID: "after", Year: 1990},
		{ID: "last", Year: 9999},
	} {
		tr.MountID, tr.RelPath, tr.Title = m.ID, tr.ID+".wav", tr.ID
		tr.Decodable, tr.DurationMS = true, 1000
		seedBrowseTrack(t, s, tr)
	}
	return s
}

func TestBrowseDecadesAndNumericOrdering(t *testing.T) {
	s := decadeBrowseFixture(t)
	want := []string{"0", "1–9", "10–19", "90–99", "100–109", "1970–1979", "1980–1989", "1990–1999", "9990–9999"}
	for _, sort := range []string{"", "name", "year"} {
		for _, direction := range []string{"asc", "desc"} {
			page := mustBrowse(t, s, BrowseQuery{Category: "years", Sort: sort, Direction: direction})
			if page.Total != len(want) || page.TotalTracks != 12 || len(page.Items) != len(want) {
				t.Fatalf("decade totals: %+v", page)
			}
			for i, item := range page.Items {
				index := i
				if direction == "desc" {
					index = len(want) - 1 - i
				}
				if item.Kind != "year" || item.Value != want[index] {
					t.Fatalf("sort %q %s index %d: %+v", sort, direction, i, item)
				}
				if item.Year > 0 {
					if item.Name != want[index] || item.YearFrom < 1 || item.YearTo > 9999 || item.Year < item.YearFrom || item.Year > item.YearTo || item.Name != fmt.Sprintf("%d–%d", item.YearFrom, item.YearTo) {
						t.Fatalf("decade metadata: %+v", item)
					}
				} else if item.Name != "未知年份" || item.YearFrom != 0 || item.YearTo != 0 || item.TrackCount != 1 || item.Initial != "#" {
					t.Fatalf("unknown decade: %+v", item)
				}
			}
		}
	}
	page := mustBrowse(t, s, BrowseQuery{Category: "years", YearFrom: browseYear(1980), YearTo: browseYear(1989)})
	if page.Total != 1 || page.TotalTracks != 3 || page.Items[0].AlbumCount != 2 || page.Items[0].TrackCount != 3 || page.Items[0].DurationMS != 3000 || page.Items[0].Year != 1989 {
		t.Fatalf("shared albums across years and artists: %+v", page)
	}
	for _, q := range []BrowseQuery{
		{Category: "years", Year: browseYear(0)},
		{Category: "years", Year: browseYear(1)},
		{Category: "years", Year: browseYear(9999)},
		{Category: "albums", YearFrom: browseYear(1980), YearTo: browseYear(1989)},
	} {
		page := mustBrowse(t, s, q)
		for _, item := range page.Items {
			data, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			knownDecade := item.Kind == "year" && item.Year > 0
			if strings.Contains(string(data), `"year_from":`) != knownDecade || strings.Contains(string(data), `"year_to":`) != knownDecade {
				t.Fatalf("range JSON fields: %s", data)
			}
		}
	}
}

func TestBrowseDecadeSearchSelectsWholeGroups(t *testing.T) {
	s := decadeBrowseFixture(t)
	for _, term := range []string{"1980", "1985", "1989", "1980-1989", "1980–1989", " 1980 - 1989 ", "1980 – 1989"} {
		for _, field := range []string{"", "name", "year"} {
			q := BrowseQuery{Category: "years", Field: field, Q: term}
			page := mustBrowse(t, s, q)
			if page.Total != 1 || page.TotalTracks != 3 || page.Items[0].Name != "1980–1989" || page.Items[0].AlbumCount != 2 {
				t.Fatalf("search %q/%s: %+v", term, field, page)
			}
			q.Page = 2 // Selecting a group must ignore the displayed page.
			ids, err := s.SelectTracks(q)
			if err != nil || !reflect.DeepEqual(ids, []string{"end", "other", "start"}) {
				t.Fatalf("whole decade selection %q/%s: %v %v", term, field, ids, err)
			}
		}
	}
	for _, term := range []string{"1980", "1985", "1980-1989"} {
		q := BrowseQuery{Category: "years", Field: "year", Q: term, Artist: browseString("Solo")}
		page := mustBrowse(t, s, q)
		ids, err := s.SelectTracks(q)
		if page.Total != 1 || page.TotalTracks != 1 || page.Items[0].YearFrom != 1980 || page.Items[0].YearTo != 1989 || err != nil || !reflect.DeepEqual(ids, []string{"other"}) {
			t.Fatalf("decade with only 1989 present %q: %+v %v %v", term, page, ids, err)
		}
	}
	for _, tc := range []struct {
		q   BrowseQuery
		ids []string
	}{
		{BrowseQuery{Category: "years", Q: "5"}, []string{"first", "ninth"}},
		{BrowseQuery{Category: "years", Q: "1-9"}, []string{"first", "ninth"}},
		{BrowseQuery{Category: "years", Q: "1990"}, []string{"after"}},
		{BrowseQuery{Category: "years", Q: "9995"}, []string{"last"}},
		{BrowseQuery{Category: "years", Q: "9990–9999"}, []string{"last"}},
		{BrowseQuery{Category: "years", Year: browseYear(0)}, []string{"unknown"}},
		{BrowseQuery{Category: "years", Year: browseYear(1989)}, []string{"end", "other"}},
		{BrowseQuery{Category: "years", Q: "1985", Field: "all"}, []string{}},
		{BrowseQuery{Category: "years", Q: "start", Field: "title"}, []string{"start"}},
		{BrowseQuery{Category: "years", Q: "%"}, []string{}},
		{BrowseQuery{Category: "years", Q: "1980' OR 1=1 --"}, []string{}},
	} {
		page := mustBrowse(t, s, tc.q)
		ids, err := s.SelectTracks(tc.q)
		if page.TotalTracks != len(tc.ids) || err != nil || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("search %+v: tracks=%d ids=%v err=%v", tc.q, page.TotalTracks, ids, err)
		}
	}
	for _, category := range []string{"artists", "albums", "tracks", "genres", "composers", "folders"} {
		for _, term := range []string{"1980", "1985", "1989", "1980-1989"} {
			q := BrowseQuery{Category: category, Field: "year", Q: term}
			page := mustBrowse(t, s, q)
			ids, err := s.SelectTracks(q)
			want := map[string]int{"1980": 1, "1989": 2}[term]
			if page.TotalTracks != want || err != nil || len(ids) != want {
				t.Fatalf("other categories retain year text search %+v: tracks=%d ids=%v err=%v", q, page.TotalTracks, ids, err)
			}
		}
	}
}

func TestBrowseYearRangeAlbumDrilldownAndSelection(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	wantIDs := make([]string, 30)
	for i := range wantIDs {
		id := fmt.Sprintf("t%02d", i)
		wantIDs[i] = id
		seedBrowseTrack(t, s, Track{ID: id, MountID: m.ID, RelPath: id + ".wav", Title: id, Artist: "Guest", AlbumArtist: "Band", Album: fmt.Sprintf("Album %02d", i), Year: 1980 + i%10, Decodable: true})
	}
	for _, tr := range []Track{
		{ID: "before", Artist: "Band", Album: "Album 00", Year: 1979, Decodable: true},
		{ID: "after", Artist: "Band", Album: "Album 00", Year: 1990, Decodable: true},
		{ID: "unknown", Artist: "Band", Album: "Album 00", Decodable: true},
		{ID: "undecodable", Artist: "Band", Album: "Album 00", Year: 1985},
		{ID: "other-artist", Artist: "Solo", Album: "Album 00", Year: 1989, Decodable: true},
	} {
		tr.MountID, tr.RelPath, tr.Title = m.ID, tr.ID+".wav", tr.ID
		seedBrowseTrack(t, s, tr)
	}
	decades := mustBrowse(t, s, BrowseQuery{Category: "years", Artist: browseString("Band"), Q: "1985"})
	if decades.Total != 1 || decades.Items[0].TrackCount != 31 || decades.Items[0].AlbumCount != 30 {
		t.Fatalf("decade album totals: %+v", decades)
	}
	decade := decades.Items[0]
	q := BrowseQuery{Category: "albums", Artist: browseString("Band"), YearFrom: &decade.YearFrom, YearTo: &decade.YearTo, PageSize: 24}
	seen := map[string]bool{}
	for pageNumber := 1; pageNumber <= 3; pageNumber++ {
		q.Page = pageNumber
		page := mustBrowse(t, s, q)
		if page.Total != 30 || page.TotalTracks != 31 || len(page.Items) != []int{24, 6, 0}[pageNumber-1] {
			t.Fatalf("range albums page %d: %+v", pageNumber, page)
		}
		for _, album := range page.Items {
			if seen[album.Value] || album.Artist != "Band" || album.YearFrom != 0 || album.YearTo != 0 {
				t.Fatalf("range album identity: %+v", album)
			}
			seen[album.Value] = true
		}
		ids, err := s.SelectTracks(q)
		if err != nil || !reflect.DeepEqual(ids, wantIDs) {
			t.Fatalf("cross-page range selection %d: %v %v", pageNumber, ids, err)
		}
	}
	if len(seen) != 30 {
		t.Fatalf("missing albums: %d", len(seen))
	}
	ids, err := s.SelectTracks(BrowseQuery{Category: "years", Artist: browseString("Band"), Q: "1985", Page: 3})
	if err != nil || !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("cross-page decade search selection: %v %v", ids, err)
	}
	for _, tc := range []struct {
		artist, album string
		mode          string
		from, to      int
		tracks        int
		ids           []string
	}{
		{"Band", "Album 00", "album", 1980, 1989, 2, []string{"t00"}},
		{"Solo", "Album 00", "album", 1980, 1989, 1, []string{"other-artist"}},
		{"Guest", "Album 00", "track", 1980, 1989, 1, []string{"t00"}},
		{"Band", "Album 00", "album", 1985, 1985, 1, []string{}},
		{"Band", "Album 09", "album", 1989, 1989, 1, []string{"t09"}},
		{"Band", "Album 00", "album", 1981, 1984, 0, []string{}},
	} {
		query := BrowseQuery{Category: "albums", Artist: &tc.artist, Album: &tc.album, ArtistMode: tc.mode, YearFrom: &tc.from, YearTo: &tc.to}
		page := mustBrowse(t, s, query)
		ids, err := s.SelectTracks(query)
		if page.TotalTracks != tc.tracks || page.Total != min(1, tc.tracks) || err != nil || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("combined filters %+v: %+v %v %v", query, page, ids, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO mounts(id,host,share,target,created_at,kind) VALUES
		('empty','localhost','Empty','/empty',1,'local'),('outside','localhost','Outside','/outside',1,'local')`); err != nil {
		t.Fatal(err)
	}
	seedBrowseTrack(t, s, Track{ID: "outside", MountID: "outside", RelPath: "outside.wav", Year: 1990, Decodable: true})
	q = BrowseQuery{Category: "folders", YearFrom: browseYear(1980), YearTo: browseYear(1989)}
	page := mustBrowse(t, s, q)
	ids, err = s.SelectTracks(q)
	if page.Total != 1 || page.TotalTracks != 32 || page.Items[0].Value != m.ID || err != nil || len(ids) != 31 {
		t.Fatalf("range sources exclude empty/unmatched: %+v %v %v", page, ids, err)
	}
	for _, term := range []string{"Empty", "Outside"} {
		q.Q = term
		page := mustBrowse(t, s, q)
		ids, err := s.SelectTracks(q)
		if page.Total != 0 || page.TotalTracks != 0 || err != nil || len(ids) != 0 {
			t.Fatalf("source search with range %q: %+v %v %v", term, page, ids, err)
		}
	}
}

func TestBrowseYearRangeExtremes(t *testing.T) {
	s := decadeBrowseFixture(t)
	for _, tc := range []struct {
		from, to int
		ids      []string
	}{
		{1, 9, []string{"first", "ninth"}},
		{1, 1, []string{"first"}},
		{9990, 9999, []string{"last"}},
		{9999, 9999, []string{"last"}},
	} {
		q := BrowseQuery{Category: "albums", YearFrom: &tc.from, YearTo: &tc.to}
		page := mustBrowse(t, s, q)
		ids, err := s.SelectTracks(q)
		if page.TotalTracks != len(tc.ids) || err != nil || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("range %d..%d: %+v %v %v", tc.from, tc.to, page, ids, err)
		}
	}
}

func TestBrowsePaginationSortAndSelection(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	for i := 0; i < 60; i++ {
		seedBrowseTrack(t, s, Track{ID: fmt.Sprintf("t%02d", i), MountID: m.ID, RelPath: fmt.Sprintf("%02d.wav", i), Title: "相同", Artist: "王菲", Album: "Album", Year: 2000 + i, AddedAt: int64(i), DurationMS: i, Decodable: i%2 == 0})
	}
	seen := map[string]bool{}
	for p := 1; p <= 3; p++ {
		page := mustBrowse(t, s, BrowseQuery{Page: p})
		if page.Total != 60 || page.TotalTracks != 60 || len(page.Items) > 24 {
			t.Fatalf("page: %+v", page)
		}
		for _, item := range page.Items {
			if seen[item.Value] {
				t.Fatalf("duplicate page ID %s", item.Value)
			}
			seen[item.Value] = true
		}
	}
	if len(seen) != 60 {
		t.Fatalf("missing paginated tracks: %d", len(seen))
	}
	for _, size := range []int{20, 24, 48, 96} {
		if page := mustBrowse(t, s, BrowseQuery{PageSize: size}); len(page.Items) != min(size, 60) {
			t.Fatalf("size %d: %+v", size, page)
		}
	}
	page := mustBrowse(t, s, BrowseQuery{PageSize: 10, Offset: 20})
	if page.Offset != 20 || page.Page != 3 || len(page.Items) != 10 || page.Items[0].Value != "t20" {
		t.Fatalf("offset page: %+v", page)
	}
	for _, sort := range []string{"year", "added", "duration"} {
		for _, direction := range []string{"asc", "desc"} {
			page := mustBrowse(t, s, BrowseQuery{Sort: sort, Direction: direction})
			want := "t00"
			if direction == "desc" {
				want = "t59"
			}
			if page.Items[0].Value != want {
				t.Fatalf("sort %s %s: %s", sort, direction, page.Items[0].Value)
			}
		}
	}
	for _, category := range []string{"tracks", "artists", "albums", "genres", "composers", "years", "folders"} {
		ids, err := s.SelectTracks(BrowseQuery{Category: category, Page: 3, PageSize: 24})
		if err != nil || len(ids) != 30 || ids[0] != "t00" || ids[29] != "t58" {
			t.Fatalf("selection %s: %v %v", category, ids, err)
		}
	}
}

func TestBrowseFoldersAndSelection(t *testing.T) {
	s, m := browseFixture(t)
	page := mustBrowse(t, s, BrowseQuery{Category: "folders", MountID: m, Directory: "中文"})
	if page.Total != 1 || page.Items[0].Kind != "folder" || page.Items[0].Directory != "中文/子目录" || page.Items[0].TrackCount != 1 {
		t.Fatalf("direct child: %+v", page)
	}
	page = mustBrowse(t, s, BrowseQuery{Category: "folders", MountID: m, Directory: "中文/子目录"})
	if page.Total != 1 || page.Items[0].Track == nil || page.Items[0].Track.ID != "a" {
		t.Fatalf("directory track: %+v", page)
	}
	page = mustBrowse(t, s, BrowseQuery{Category: "folders", MountID: m, Q: "子目录", Recursive: true})
	if page.Total != 2 || page.TotalTracks != 1 {
		t.Fatalf("recursive search: %+v", page)
	}
	for _, tc := range []struct {
		q BrowseQuery
		n int
	}{
		{BrowseQuery{Category: "folders", MountID: m}, 2},
		{BrowseQuery{Category: "folders", MountID: m, Recursive: true}, 4},
		{BrowseQuery{Category: "tracks", MountID: m, Directory: "中文"}, 0},
		{BrowseQuery{Category: "tracks", MountID: m, Directory: "中文", Recursive: true}, 1},
		{BrowseQuery{Category: "albums", Artist: browseString("王菲"), Album: browseString("同名")}, 2},
	} {
		ids, err := s.SelectTracks(tc.q)
		if err != nil || len(ids) != tc.n {
			t.Fatalf("selection %+v: %v %v", tc.q, ids, err)
		}
	}
	_, err := s.db.Exec(`INSERT INTO mounts(id,host,share,target,created_at,kind) VALUES('empty','localhost','Empty','/empty',1,'local')`)
	if err != nil {
		t.Fatal(err)
	}
	page = mustBrowse(t, s, BrowseQuery{Category: "folders", Q: "Empty"})
	if page.Total != 1 || page.TotalTracks != 0 || page.Items[0].TrackCount != 0 {
		t.Fatalf("empty source: %+v", page)
	}
}

func TestBrowseRejectsInvalidQuery(t *testing.T) {
	s, _ := browseFixture(t)
	for _, q := range []BrowseQuery{
		{Category: "sql"}, {Field: "title; DROP TABLE tracks"}, {Sort: "title"}, {Direction: "ASC;--"}, {ArtistMode: "both"},
		{Page: -1}, {PageSize: -24}, {PageSize: 201}, {Offset: -1}, {Page: int(^uint(0) >> 1)}, {Initial: "AB"}, {Initial: "1"},
		{Directory: "../outside", MountID: "m"}, {Directory: "nested/../outside", MountID: "m"}, {Directory: "/absolute", MountID: "m"},
		{Directory: "C:\\outside", MountID: "m"}, {Directory: "album"}, {Year: browseYear(-1)},
		{YearFrom: browseYear(1980)}, {YearTo: browseYear(1989)},
		{YearFrom: browseYear(0), YearTo: browseYear(9)},
		{YearFrom: browseYear(-1), YearTo: browseYear(1989)},
		{YearFrom: browseYear(1), YearTo: browseYear(0)},
		{YearFrom: browseYear(1), YearTo: browseYear(-1)},
		{YearFrom: browseYear(9990), YearTo: browseYear(10000)},
		{YearFrom: browseYear(10000), YearTo: browseYear(10000)},
		{YearFrom: browseYear(1990), YearTo: browseYear(1989)},
		{YearFrom: browseYear(1980), YearTo: browseYear(1989), Year: browseYear(1985)},
		{YearFrom: browseYear(1), YearTo: browseYear(9), Year: browseYear(0)},
	} {
		if _, err := s.Browse(q); err == nil {
			t.Fatalf("Browse accepted %+v", q)
		}
		if _, err := s.SelectTracks(q); err == nil {
			t.Fatalf("SelectTracks accepted %+v", q)
		}
	}
}

func TestSelectTracksLimitIsNotSilent(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	_, err := s.db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<10001)
		INSERT INTO tracks(id,mount_id,path,rel_path,title,scanned_at,decodable)
		SELECT 't'||i,?,'/test/'||i,i||'.wav','Track',1,1 FROM n`, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := s.SelectTracks(BrowseQuery{}); err == nil || ids != nil {
		t.Fatalf("must reject overflow: len=%d err=%v", len(ids), err)
	}
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE id='t10001'`); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.SelectTracks(BrowseQuery{}); err != nil || len(ids) != 10000 {
		t.Fatalf("exact limit: len=%d err=%v", len(ids), err)
	}
}
