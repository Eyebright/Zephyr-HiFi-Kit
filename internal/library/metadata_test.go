package library

import (
	"reflect"
	"testing"
)

func applyMetadataPaths(tracks []Track) []Track {
	resolver := newMetadataPathResolver(tracks)
	for i := range tracks {
		resolver.apply(&tracks[i])
	}
	return tracks
}

func TestMetadataPathResolverStandardLayouts(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Artist/Album/01 First.wav"},
		{RelPath: "Artist/Album/Disc 2/03 Second.wav"},
		{RelPath: "Hi-Res/Artist/Album [DSD64]/04 Third.dsf"},
		{RelPath: "H-098-[DSD-DSF] THREESOME - Whatever!/H-098-[DSD-DSF] THREESOME - Whatever!/001 Fourth.dsf"},
	})
	want := []Track{
		{Title: "First", Artist: "Artist", Album: "Album", AlbumArtist: "Artist", TrackNumber: 1},
		{Title: "Second", Artist: "Artist", Album: "Album", AlbumArtist: "Artist", DiscNumber: 2, TrackNumber: 3},
		{Title: "Third", Artist: "Artist", Album: "Album", AlbumArtist: "Artist", TrackNumber: 4},
		{Title: "Fourth", Artist: "THREESOME", Album: "Whatever!", AlbumArtist: "THREESOME", TrackNumber: 1},
	}
	for i, tr := range tracks {
		got := Track{Title: tr.Title, Artist: tr.Artist, Album: tr.Album, AlbumArtist: tr.AlbumArtist, DiscNumber: tr.DiscNumber, TrackNumber: tr.TrackNumber}
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("track %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestMetadataPathResolverFlatSingles(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "01 Artist - First.wav"},
		{RelPath: "100首极品外语无损单曲音乐/Second.wav"},
		{RelPath: "100首极品外语无损单曲音乐/Third.wav"},
		{RelPath: "余又-单曲合集/01 Fourth.ogg"},
	})
	wantArtist := []string{"Artist", "", "", "余又"}
	wantAlbum := []string{
		unidentifiedAlbumPrefix + "First",
		unidentifiedAlbumPrefix + "Second",
		unidentifiedAlbumPrefix + "Third",
		unidentifiedAlbumPrefix + "Fourth",
	}
	for i, tr := range tracks {
		if tr.Artist != wantArtist[i] || tr.Album != wantAlbum[i] || tr.AlbumArtist != "" {
			t.Errorf("track %d = artist %q album %q album artist %q", i, tr.Artist, tr.Album, tr.AlbumArtist)
		}
	}
}

func TestMetadataPathResolverNumberedAlbumOnly(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Album Only/01 One.wav"},
		{RelPath: "Album Only/02 Two.wav"},
		{RelPath: "Ambiguous/One.wav"},
		{RelPath: "Ambiguous/02 Two.wav"},
	})
	if tracks[0].Album != "Album Only" || tracks[1].Album != "Album Only" {
		t.Fatalf("numbered album folder not inferred: %+v", tracks[:2])
	}
	if tracks[2].Album != unidentifiedAlbumPrefix+"One" || tracks[3].Album != unidentifiedAlbumPrefix+"Two" {
		t.Fatalf("ambiguous folder grouped as an album: %+v", tracks[2:])
	}
}

func TestMetadataPathResolverLiteralHashAndCueSuffix(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Love #1.wav"},
		{RelPath: "Love #01.wav"},
		{RelPath: "Artist/Album/image.wav#01", CueStart: 1, TrackNumber: 1},
	})
	if tracks[0].Title != "Love #1" || tracks[1].Title != "Love #01" {
		t.Fatalf("literal hash titles changed: %+v", tracks[:2])
	}
	if tracks[2].Title != "image" || tracks[2].Album != "Album" || tracks[2].TrackNumber != 1 {
		t.Fatalf("cue virtual track wrong: %+v", tracks[2])
	}
}

func TestMetadataPathResolverSinglesDirectories(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Singles/01 One.wav"},
		{RelPath: "Singles/02 Two.wav"},
		{RelPath: "Artist/Singles/01 Three.wav"},
		{RelPath: "Artist/Singles/02 Four.wav"},
	})
	for i, want := range []string{"One", "Two", "Three", "Four"} {
		if tracks[i].Album != unidentifiedAlbumPrefix+want {
			t.Errorf("track %d album = %q", i, tracks[i].Album)
		}
	}
	if tracks[2].Artist != "Artist" || tracks[3].Artist != "Artist" {
		t.Fatalf("artist singles parent not inferred: %+v", tracks[2:])
	}
}

func TestMetadataScanResolverUsesLiveRows(t *testing.T) {
	t.Run("deleted rows", func(t *testing.T) {
		previous := []Track{
			{RelPath: "Album/90 Old A.wav", TrackNumber: 90},
			{RelPath: "Album/91 Old B.wav", TrackNumber: 91},
			{RelPath: "Album/92 Old C.wav", TrackNumber: 92},
			{RelPath: "Album/93 Old D.wav", TrackNumber: 93},
		}
		changed := []Track{{RelPath: "Album/Song.wav"}}
		remaining := map[string]Track{}
		for _, tr := range previous {
			remaining[tr.RelPath] = tr
		}
		resolver := metadataScanResolver(previous, changed, remaining)
		resolver.apply(&changed[0])
		if changed[0].Album != unidentifiedAlbumPrefix+"Song" {
			t.Fatalf("deleted rows influenced album: %+v", changed[0])
		}
	})
	t.Run("changed rows override previous", func(t *testing.T) {
		previous := []Track{
			{RelPath: "Album/One.wav", TrackNumber: 1},
			{RelPath: "Album/Two.wav", TrackNumber: 2},
		}
		changed := []Track{{RelPath: "Album/One.wav"}}
		resolver := metadataScanResolver(previous, changed, map[string]Track{})
		resolver.apply(&changed[0])
		if changed[0].Album != unidentifiedAlbumPrefix+"One" {
			t.Fatalf("previous row overrode changed row: %+v", changed[0])
		}
	})
}

func TestMetadataPathResolverPreservesTagsAndFillsMissing(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{
			RelPath: "Path Artist/Path Album/01 File Artist - File Title.wav",
			Title:   "Tag Title", Artist: "Tag Artist", Album: "Tag Album", AlbumArtist: "Tag Album Artist",
			DiscNumber: 4, TrackNumber: 9,
		},
		{
			RelPath: "Path Artist/Path Album/02 Song.wav",
			Title:   "Kept Title", Artist: "Kept Artist",
		},
	})
	if tracks[0].Title != "Tag Title" || tracks[0].Artist != "Tag Artist" || tracks[0].Album != "Tag Album" ||
		tracks[0].AlbumArtist != "Tag Album Artist" || tracks[0].DiscNumber != 4 || tracks[0].TrackNumber != 9 {
		t.Fatalf("valid tags changed: %+v", tracks[0])
	}
	if tracks[1].Title != "Kept Title" || tracks[1].Artist != "Kept Artist" || tracks[1].Album != "Path Album" || tracks[1].AlbumArtist != "Path Artist" {
		t.Fatalf("missing fields not filled conservatively: %+v", tracks[1])
	}
}

func TestMetadataPathResolverCompilation(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Collection/01 A - One.wav"},
		{RelPath: "Collection/02 B - Two.wav"},
		{RelPath: "Collection/03 Untagged.wav"},
	})
	for i := 0; i < 2; i++ {
		if tracks[i].Album != "Collection" || tracks[i].AlbumArtist != "群星" {
			t.Errorf("compilation track %d = %+v", i, tracks[i])
		}
	}
	if tracks[2].Album != "Collection" || tracks[2].Artist != "" || tracks[2].AlbumArtist != "群星" {
		t.Errorf("compilation missing artist = %+v", tracks[2])
	}
}

func TestMetadataPathResolverUnreliableNestedPath(t *testing.T) {
	tracks := applyMetadataPaths([]Track{
		{RelPath: "Category/[DSD]/song.wav"},
	})
	if tracks[0].Album != unidentifiedAlbumPrefix+"Category/[DSD]" {
		t.Fatalf("unreliable path not split by source folder: %+v", tracks[0])
	}
}
