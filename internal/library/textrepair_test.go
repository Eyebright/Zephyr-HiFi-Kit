package library

import (
	"context"
	"strings"
	"testing"
)

func TestRepairTrackTextFallbacks(t *testing.T) {
	title, artist, album, albumArtist := repairTrackText("????", "??", "????15", "??", "英文人声/01 - Dreaming My Dreams 梦中有你.wav")
	if title != "Dreaming My Dreams 梦中有你" {
		t.Fatalf("title = %q", title)
	}
	if album != "英文人声" {
		t.Fatalf("album = %q", album)
	}
	if artist != "" || albumArtist != "" {
		t.Fatalf("artist=%q albumArtist=%q", artist, albumArtist)
	}

	title, artist, _, _ = repairTrackText("????", "???", "", "", "Rock/07 Artist Name - Song.flac")
	if title != "Artist Name - Song" || artist != "Artist Name" {
		t.Fatalf("artist/title fallback = %q / %q", title, artist)
	}

	if damagedTagText("What?") || damagedTagText("Donna Donna") {
		t.Fatal("single question marks in prose must not count as damage")
	}
	if !damagedTagText("????") || !damagedTagText("??") {
		t.Fatal("fully replaced text must count as damage")
	}
	if !damagedTagText("Dreaming My Derams With You ????") {
		t.Fatal("a trailing replacement run must count as damage")
	}
}

func TestPreviewAndApplyTextRepair(t *testing.T) {
	s, m := browseFixture(t)
	seedBrowseTrack(t, s, Track{
		ID: "bad1", MountID: m, RelPath: "英文人声/01 - Dreaming My Dreams 梦中有你.wav",
		Title: "????", Artist: "??", Album: "????15", AlbumArtist: "??",
		Codec: "pcm_s16le", DurationMS: 1000, AddedAt: 5,
	})
	preview, err := s.PreviewTextRepair(5)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count < 1 || len(preview.Samples) == 0 {
		t.Fatalf("preview = %+v", preview)
	}
	sample := preview.Samples[0]
	if sample.NewTitle == "" || !strings.Contains(sample.NewTitle, "梦中有你") {
		t.Fatalf("sample = %+v", sample)
	}
	n, err := s.ApplyTextRepair(context.Background())
	if err != nil || n < 1 {
		t.Fatalf("apply = %d, %v", n, err)
	}
	var title, artist, album, albumArtist string
	if err := s.db.QueryRow(`SELECT title,artist,album,album_artist FROM tracks WHERE id='bad1'`).Scan(&title, &artist, &album, &albumArtist); err != nil {
		t.Fatal(err)
	}
	if title != "Dreaming My Dreams 梦中有你" || album != "英文人声" || artist != "" || albumArtist != "" {
		t.Fatalf("stored = %q / %q / %q / %q", title, artist, album, albumArtist)
	}
	again, err := s.PreviewTextRepair(5)
	if err != nil || again.Count != 0 {
		t.Fatalf("second preview = %+v, %v", again, err)
	}
}
