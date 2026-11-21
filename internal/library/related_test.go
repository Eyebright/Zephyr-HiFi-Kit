package library

import "testing"

func seedRelatedTrack(t *testing.T, s *Service, mountID string, tr Track) {
	t.Helper()
	tr.MountID = mountID
	tr.Decodable = true
	if tr.RelPath == "" {
		tr.RelPath = tr.ID + ".flac"
	}
	seedBrowseTrack(t, s, tr)
}

func TestBrowseAlbumCarriesGenreAndFavorite(t *testing.T) {
	s, _ := browseFixture(t)
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "album", Artist: favoriteString("王菲"), Album: favoriteString("同名")})
	page := mustBrowse(t, s, BrowseQuery{Category: "albums"})
	for _, item := range page.Items {
		switch item.Album {
		case "同名":
			if item.Artist == "王菲" {
				if item.Genre == "" || !item.Favorite {
					t.Fatalf("favorited album missing genre/favorite: %+v", item)
				}
			} else if item.Artist == "陈奕迅" && item.Favorite {
				t.Fatalf("non-favorited album reported favorite: %+v", item)
			}
		}
	}
}

func TestRelatedAlbumsScoreArtistGenreYear(t *testing.T) {
	s, m := browseFixture(t)
	for _, tr := range []Track{
		{ID: "r1", Title: "B1", Artist: "王菲", AlbumArtist: "王菲", Album: "专辑B", Genre: "Jazz", Year: 2003, DurationMS: 1000, AddedAt: 60, CoverHash: "b-cover"},
		{ID: "r2", Title: "C1", Artist: "王菲", AlbumArtist: "王菲", Album: "专辑C", Genre: "Pop", Year: 2005, DurationMS: 1000, AddedAt: 70},
		{ID: "r3", Title: "D1", Artist: "陈奕迅", AlbumArtist: "陈奕迅", Album: "专辑D", Genre: "Jazz", Year: 2002, DurationMS: 1000, AddedAt: 80},
	} {
		seedRelatedTrack(t, s, m, tr)
	}
	items, err := s.RelatedAlbums("王菲", "同名", 6)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		album string
		score int
	}{{"专辑B", 7}, {"专辑C", 4}, {"专辑D", 3}}
	if len(items) != len(want) {
		t.Fatalf("related = %+v", items)
	}
	for i, w := range want {
		if items[i].Album != w.album || items[i].Score != w.score {
			t.Fatalf("related[%d] = %+v, want %s score %d", i, items[i], w.album, w.score)
		}
	}

	groups, err := s.RelatedAlbumsByArtist("王菲", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups["同名"]) != 2 || groups["同名"][0].Album != "专辑B" || groups["同名"][1].Album != "专辑C" {
		t.Fatalf("batch for 同名 = %+v", groups["同名"])
	}
	if len(groups["专辑B"]) == 0 {
		t.Fatalf("batch missing 专辑B strip: %+v", groups)
	}
}

func TestFavoriteAlbumsList(t *testing.T) {
	s, _ := browseFixture(t)
	mustSaveFavorite(t, s, FavoriteRequest{Kind: "album", Artist: favoriteString("王菲"), Album: favoriteString("同名")})
	albums, err := s.ListFavoriteAlbums()
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].Artist != "王菲" || albums[0].Album != "同名" {
		t.Fatalf("favorite albums = %+v", albums)
	}
}

func TestRelatedAlbumsForPairs(t *testing.T) {
	s, m := browseFixture(t)
	for _, tr := range []Track{
		{ID: "p1", Title: "B1", Artist: "王菲", AlbumArtist: "王菲", Album: "专辑B", Genre: "Jazz", Year: 2003, DurationMS: 1000, AddedAt: 60, CoverHash: "b-cover"},
		{ID: "p2", Title: "D1", Artist: "陈奕迅", AlbumArtist: "陈奕迅", Album: "专辑D", Genre: "Pop", Year: 2002, DurationMS: 1000, AddedAt: 80},
	} {
		seedRelatedTrack(t, s, m, tr)
	}
	groups, err := s.RelatedAlbumsForPairs([]AlbumPair{{Artist: "王菲", Album: "同名"}, {Artist: "陈奕迅", Album: "同名"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	found := map[string][]RelatedAlbum{}
	for _, g := range groups {
		found[g.Artist] = g.Items
	}
	if len(found["王菲"]) != 2 || found["王菲"][0].Album != "专辑B" || found["王菲"][1].Album != "专辑D" {
		t.Fatalf("王菲 related = %+v", found["王菲"])
	}
	if len(found["陈奕迅"]) == 0 || found["陈奕迅"][0].Album != "专辑D" {
		t.Fatalf("陈奕迅 related = %+v", found["陈奕迅"])
	}
}
