package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/store"
)

func favoriteString(s string) *string { return &s }

func favoriteFixture(t *testing.T) (*Service, string) {
	t.Helper()
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := insertMounted(t, s, t.TempDir())
	return s, m.ID
}

func favoriteExec(t *testing.T, s *Service, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func seedFavoriteTrack(t *testing.T, s *Service, mountID, id, title, artist, albumArtist, album string, decodable bool) {
	t.Helper()
	favoriteExec(t, s, `INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album_artist,album,cover_hash,duration_ms,codec,decodable,scanned_at)
		VALUES(?,?,?,?,?,?,?,?,?,60000,'flac',?,1)`, id, mountID, "/test/"+id+".flac", id+".flac", title, artist, albumArtist, album, "cover-"+id, boolInt(decodable))
}

func mustFavoritePage(t *testing.T, s *Service, q FavoriteQuery) FavoritePage {
	t.Helper()
	p, err := s.ListFavorites(q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustSaveFavorite(t *testing.T, s *Service, req FavoriteRequest) {
	t.Helper()
	if err := s.SaveFavorite(req); err != nil {
		t.Fatal(err)
	}
}

func TestFavoriteTrackSnapshotsIdempotencyAndDelete(t *testing.T) {
	s, m := favoriteFixture(t)
	seedFavoriteTrack(t, s, m, "a", "Original", "Guest", "Band", "Album", true)
	seedFavoriteTrack(t, s, m, "b", "Second", "Other", "", "Other album", false)
	a := FavoriteRequest{Kind: "track", TrackID: "a"}
	mustSaveFavorite(t, s, a)
	favoriteExec(t, s, `UPDATE favorites SET added_at=10`)
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "track", TrackID: "b"})
	favoriteExec(t, s, `UPDATE favorites SET added_at=20 WHERE track_id='b'`)
	before := mustFavoritePage(t, s, FavoriteQuery{})
	mustSaveFavorite(t, s, a)
	after := mustFavoritePage(t, s, FavoriteQuery{})
	if !reflect.DeepEqual(before, after) || after.Total != 2 || after.Items[0].TrackID != "b" {
		t.Fatalf("duplicate PUT changed ordering: before=%+v after=%+v", before, after)
	}
	if after.Items[1].Artist != "Guest" || after.Items[1].Missing || !after.Items[1].Decodable || after.Items[1].TrackCount != 1 {
		t.Fatalf("track resolution: %+v", after)
	}
	favoriteExec(t, s, `UPDATE tracks SET title='Live title',artist='Live artist',cover_hash='live-cover',duration_ms=90000,decodable=0 WHERE id='a'`)
	mustSaveFavorite(t, s, a)
	live := mustFavoritePage(t, s, FavoriteQuery{Q: "Live title"})
	if live.Total != 1 || live.Items[0].Artist != "Live artist" || live.Items[0].DurationMS != 90000 || live.Items[0].Decodable {
		t.Fatalf("live metadata: %+v", live)
	}
	favoriteExec(t, s, `DELETE FROM tracks WHERE id='a'`)
	missing := mustFavoritePage(t, s, FavoriteQuery{Q: "Original"})
	if missing.Total != 1 || !missing.Items[0].Missing || missing.Items[0].Decodable || missing.Items[0].TrackCount != 0 ||
		missing.Items[0].Artist != "Guest" || missing.Items[0].Album != "Album" || missing.Items[0].CoverHash != "cover-a" || missing.Items[0].DurationMS != 60000 || missing.Items[0].AddedAt != 10 {
		t.Fatalf("missing snapshot: %+v", missing)
	}
	var codec string
	var decodable int
	if err := s.db.QueryRow(`SELECT codec,decodable FROM favorites WHERE track_id='a'`).Scan(&codec, &decodable); err != nil || codec != "flac" || decodable != 1 {
		t.Fatalf("snapshot was refreshed: %s %d %v", codec, decodable, err)
	}
	mustSaveFavorite(t, s, a) // A missing, already saved identity is still idempotent.
	if _, err := s.GetFavoriteState("a"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing state inferred from snapshot: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.DeleteFavorite(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveFavorite(a); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("new missing favorite accepted: %v", err)
	}
	if p := mustFavoritePage(t, s, FavoriteQuery{}); p.Total != 1 {
		t.Fatalf("delete failed: %+v", p)
	}
}

func TestFavoriteAlbumArtistIdentityAndLiveCounts(t *testing.T) {
	s, m := favoriteFixture(t)
	seedFavoriteTrack(t, s, m, "guest", "Song", "Guest", "Band", "Shared", true)
	seedFavoriteTrack(t, s, m, "fallback", "Song2", "Band", "", "Shared", true)
	seedFavoriteTrack(t, s, m, "unsupported", "Song3", "Guest2", "Band", "Shared", false)
	seedFavoriteTrack(t, s, m, "other", "Song4", "Other", "", "Shared", true)
	seedFavoriteTrack(t, s, m, "unknown", "Unknown", "", "", "", true)
	album := FavoriteRequest{Kind: "album", Artist: favoriteString("Band"), Album: favoriteString("Shared")}
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "album", TrackID: "guest"})
	favoriteExec(t, s, `UPDATE favorites SET added_at=10`)
	mustSaveFavorite(t, s, album)
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "album", TrackID: "fallback"})
	p := mustFavoritePage(t, s, FavoriteQuery{Kind: "album"})
	if p.Total != 1 || p.Items[0].Artist != "Band" || p.Items[0].Title != "Shared" || p.Items[0].Album != "Shared" ||
		p.Items[0].TrackCount != 3 || p.Items[0].Missing || !p.Items[0].Decodable || p.Items[0].DurationMS != 180000 || p.Items[0].AddedAt != 10 || p.Items[0].TrackID != "" {
		t.Fatalf("album identity/counts: %+v", p)
	}
	for _, id := range []string{"guest", "fallback", "unsupported"} {
		state, err := s.GetFavoriteState(id)
		if err != nil || state.Track || !state.Album || state.Artist != "Band" || state.AlbumName != "Shared" {
			t.Fatalf("state %s: %+v %v", id, state, err)
		}
	}
	state, err := s.GetFavoriteState("other")
	if err != nil || state.Album || state.Artist != "Other" {
		t.Fatalf("same title merged artists: %+v %v", state, err)
	}
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "track", TrackID: "guest"})
	state, err = s.GetFavoriteState("guest")
	if err != nil || !state.Track || !state.Album || state.Artist != "Band" {
		t.Fatalf("both states: %+v %v", state, err)
	}
	if p := mustFavoritePage(t, s, FavoriteQuery{}); p.Items[0].Artist != "Guest" {
		t.Fatalf("track uses album artist: %+v", p)
	}
	if p := mustFavoritePage(t, s, FavoriteQuery{Kind: "album", Q: "Guest"}); p.Total != 0 {
		t.Fatalf("album search uses track artists: %+v", p)
	}
	favoriteExec(t, s, `DELETE FROM tracks WHERE id IN ('guest','fallback')`)
	p = mustFavoritePage(t, s, FavoriteQuery{Kind: "album"})
	if p.Items[0].TrackCount != 1 || p.Items[0].Missing || p.Items[0].Decodable {
		t.Fatalf("current unplayable album: %+v", p)
	}
	favoriteExec(t, s, `DELETE FROM tracks WHERE id='unsupported'`)
	p = mustFavoritePage(t, s, FavoriteQuery{Kind: "album"})
	if p.Items[0].TrackCount != 0 || !p.Items[0].Missing || p.Items[0].Decodable || p.Items[0].CoverHash == "" || p.Items[0].DurationMS != 180000 {
		t.Fatalf("missing album: %+v", p)
	}
	mustSaveFavorite(t, s, album)
	seedFavoriteTrack(t, s, m, "restored", "New member", "Guest", "Band", "Shared", true)
	p = mustFavoritePage(t, s, FavoriteQuery{Kind: "album"})
	if p.Items[0].TrackCount != 1 || p.Items[0].Missing || !p.Items[0].Decodable || p.Items[0].AddedAt != 10 {
		t.Fatalf("album restoration: %+v", p)
	}
	favoriteExec(t, s, `DELETE FROM tracks WHERE id='restored'`)
	for i := 0; i < 2; i++ {
		if err := s.DeleteFavorite(album); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveFavorite(album); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("nonexistent album accepted: %v", err)
	}
	if err := s.SaveFavorite(FavoriteRequest{Kind: "album", TrackID: "guest"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing track inferred album: %v", err)
	}
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "album", Artist: favoriteString(""), Album: favoriteString("")})
	state, err = s.GetFavoriteState("unknown")
	if err != nil || !state.Album || state.Artist != "" || state.AlbumName != "" {
		t.Fatalf("explicit unknown album identity: %+v %v", state, err)
	}
}

func TestFavoritePaginationLiteralSearchAndValidation(t *testing.T) {
	s, m := favoriteFixture(t)
	empty := mustFavoritePage(t, s, FavoriteQuery{})
	if empty.Items == nil || empty.Total != 0 || empty.Page != 1 || empty.PageSize != 24 {
		t.Fatalf("empty defaults: %+v", empty)
	}
	for i := 0; i < 31; i++ {
		id := fmt.Sprintf("t%02d", i)
		title := id
		if i == 5 {
			title = `100%_live\set`
		}
		seedFavoriteTrack(t, s, m, id, title, "Guest", "Band", "Collection", true)
		mustSaveFavorite(t, s, FavoriteRequest{Kind: "track", TrackID: id})
	}
	// Deterministic tie ordering, including across page boundaries.
	favoriteExec(t, s, `UPDATE favorites SET added_at=1`)
	for _, size := range []int{6, 24, 48, 96} {
		seen := map[string]bool{}
		for page := 1; page <= 31/size+1; page++ {
			p := mustFavoritePage(t, s, FavoriteQuery{Page: page, PageSize: size})
			if p.Total != 31 || p.Page != page || p.PageSize != size {
				t.Fatalf("page metadata: %+v", p)
			}
			for j, item := range p.Items {
				want := fmt.Sprintf("t%02d", 30-(page-1)*size-j)
				if item.TrackID != want || seen[item.TrackID] {
					t.Fatalf("page gap/overlap: got=%s want=%s seen=%v", item.TrackID, want, seen)
				}
				seen[item.TrackID] = true
			}
		}
		if len(seen) != 31 {
			t.Fatalf("lost items at size %d: %v", size, seen)
		}
	}
	for term, count := range map[string]int{"%": 1, "_": 1, `\`: 1, "%_": 1, "Guest": 31, "Collection": 31, "Band": 0, "%' OR 1=1 --": 0} {
		p := mustFavoritePage(t, s, FavoriteQuery{Q: term})
		if p.Total != count {
			t.Fatalf("literal search %q: %+v", term, p)
		}
	}
	p := mustFavoritePage(t, s, FavoriteQuery{Page: 100})
	if p.Total != 31 || len(p.Items) != 0 || p.Items == nil {
		t.Fatalf("out-of-range page: %+v", p)
	}
	for _, q := range []FavoriteQuery{
		{Kind: "artist"}, {Page: -1}, {Page: int(^uint(0) >> 1)}, {PageSize: -1}, {PageSize: 25},
		{Q: strings.Repeat("a", 1025)}, {Q: "\x00"}, {Q: string([]byte{0xff})},
	} {
		if _, err := s.ListFavorites(q); err == nil {
			t.Fatalf("invalid query accepted: %+v", q)
		}
	}
	for _, req := range []FavoriteRequest{
		{}, {Kind: "track"}, {Kind: "track", TrackID: " "}, {Kind: "track", TrackID: "\x00"},
		{Kind: "track", TrackID: "t00", Artist: favoriteString("Guest")}, {Kind: "album"},
		{Kind: "album", Artist: favoriteString("Band")}, {Kind: "album", TrackID: "t00", Artist: favoriteString("Band"), Album: favoriteString("Collection")},
		{Kind: "track", TrackID: strings.Repeat("x", 513)}, {Kind: "album", Artist: favoriteString(strings.Repeat("x", 4097)), Album: favoriteString("")},
	} {
		if err := s.SaveFavorite(req); err == nil {
			t.Fatalf("invalid save accepted: %+v", req)
		}
		if err := s.DeleteFavorite(req); err == nil {
			t.Fatalf("invalid delete accepted: %+v", req)
		}
	}
	if err := s.DeleteFavorite(FavoriteRequest{Kind: "album", TrackID: "t00"}); err == nil {
		t.Fatal("album deletion should use its persistent artist+album identity")
	}
}

func TestFavoriteStableTrackRestoresAfterRescan(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	target := t.TempDir()
	m := insertMounted(t, s, target)
	path := filepath.Join(target, "song.wav")
	buildWAV(t, path)
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("scan: %+v %v", tracks, err)
	}
	id := tracks[0].ID
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "track", TrackID: id})
	before := mustFavoritePage(t, s, FavoriteQuery{}).Items[0]
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if f := mustFavoritePage(t, s, FavoriteQuery{}).Items[0]; !f.Missing || f.Title != before.Title || f.AddedAt != before.AddedAt {
		t.Fatalf("missing after scan: %+v", f)
	}
	buildWAV(t, path)
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	after := mustFavoritePage(t, s, FavoriteQuery{}).Items[0]
	if after.TrackID != id || after.Missing || !after.Decodable || after.AddedAt != before.AddedAt {
		t.Fatalf("stable favorite did not resolve: before=%+v after=%+v", before, after)
	}
}

func TestFavoriteConcurrentSaveAndSchemaReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	s := New(st, nil, "")
	m := insertMounted(t, s, t.TempDir())
	seedFavoriteTrack(t, s, m.ID, "a", "One", "Artist", "", "Album", true)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.SaveFavorite(FavoriteRequest{Kind: "track", TrackID: "a"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	before := mustFavoritePage(t, s, FavoriteQuery{})
	if before.Total != 1 {
		t.Fatalf("concurrent duplicate favorites: %+v", before)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st = reopened
	s = New(st, nil, "")
	after := mustFavoritePage(t, s, FavoriteQuery{})
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("migration altered favorites: before=%+v after=%+v", before, after)
	}
	favoriteExec(t, s, `DELETE FROM tracks`)
	favoriteExec(t, s, `DELETE FROM mounts`)
	if p := mustFavoritePage(t, s, FavoriteQuery{}); p.Total != 1 || !p.Items[0].Missing {
		t.Fatalf("favorite must survive source deletion: %+v", p)
	}
}
