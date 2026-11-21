package library

import (
	"context"
	"testing"
)

func TestArtistIndexMaterialized(t *testing.T) {
	s, _ := browseFixture(t)
	if err := s.RebuildArtistIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	idx, err := s.ArtistIndex(context.Background(), "album")
	if err != nil {
		t.Fatal(err)
	}
	if idx.Total != 4 || idx.Version == 0 {
		t.Fatalf("index = %+v", idx)
	}
	names := []string{}
	for _, item := range idx.Items {
		names = append(names, item.Name)
	}
	want := []string{"", "ABC", "陈奕迅", "王菲"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v", names)
		}
	}
	seen := map[string]InitialCount{}
	for _, c := range idx.Initials {
		seen[c.Key] = c
	}
	if seen["A"].Count != 1 || seen["C"].Count != 1 || seen["W"].Count != 1 || seen["#"].Count != 1 {
		t.Fatalf("initials = %+v", idx.Initials)
	}
	for _, item := range idx.Items {
		if item.Name == "王菲" {
			if item.TrackCount != 2 || item.Initial != "W" || item.FirstChar != "王" {
				t.Fatalf("王菲 = %+v", item)
			}
		}
	}
	track, err := s.ArtistIndex(context.Background(), "track")
	if err != nil || track.Total != 5 || track.Mode != "track" {
		t.Fatalf("track index = %+v err=%v", track, err)
	}
}
