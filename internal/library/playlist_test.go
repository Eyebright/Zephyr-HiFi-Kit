package library

import (
	"errors"
	"testing"
	"time"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/store"
)

func seedTrack(t *testing.T, st *store.Store, mountID, id, title string) {
	t.Helper()
	_, err := st.DB.Exec(`INSERT INTO tracks
		(id,mount_id,path,rel_path,title,artist,album,duration_ms,decodable,scanned_at)
		VALUES(?,?,?,?,?,?,?,?,1,?)`,
		id, mountID, "/music/"+id+".flac", id+".flac", title, "Artist", "Album", 60000, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlaylistLifecycle(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	seedTrack(t, st, m.ID, "t1", "One")
	seedTrack(t, st, m.ID, "t2", "Two")

	if _, err := s.CreatePlaylist("  "); err == nil {
		t.Fatal("empty name must be rejected")
	}
	p, err := s.CreatePlaylist("晚间爵士")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPlaylistItems(p.ID, []string{"t1", "nope"}); err == nil {
		t.Fatal("unknown track must be rejected")
	}
	d, err := s.AddPlaylistItems(p.ID, []string{"t1", "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 2 || d.Items[0].Title != "One" || d.Items[0].Missing {
		t.Fatalf("items not resolved: %+v", d.Items)
	}

	// Reorder, then remove the first entry.
	d, err = s.SetPlaylistItems(p.ID, []string{"t2", "t1"})
	if err != nil || d.Items[0].TrackID != "t2" {
		t.Fatalf("reorder failed: %+v %v", d.Items, err)
	}
	d, err = s.RemovePlaylistItem(p.ID, 0)
	if err != nil || len(d.Items) != 1 || d.Items[0].TrackID != "t1" {
		t.Fatalf("remove failed: %+v %v", d.Items, err)
	}
	if _, err := s.RemovePlaylistItem(p.ID, 5); err == nil {
		t.Fatal("out-of-range remove must fail")
	}

	if err := s.RenamePlaylist(p.ID, "深夜爵士"); err != nil {
		t.Fatal(err)
	}
	ps, err := s.ListPlaylists()
	if err != nil || len(ps) != 1 || ps[0].Name != "深夜爵士" || ps[0].Count != 1 {
		t.Fatalf("list mismatch: %+v %v", ps, err)
	}
	if err := s.DeletePlaylist(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPlaylist(p.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM playlist_items`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("items must cascade delete, got %d (%v)", n, err)
	}
}

func TestHistoryResolvedAndFiltered(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	seedTrack(t, st, m.ID, "t1", "One")
	now := time.Now().Unix()
	for _, h := range []struct {
		id, node, track string
		at              int64
	}{
		{"h1", "n1", "t1", now - 20},
		{"h2", "n2", "gone", now - 10},
		{"h3", "n1", "t1", now},
	} {
		if _, err := st.DB.Exec(`INSERT INTO play_history(id,node_id,track_id,started_at) VALUES(?,?,?,?)`,
			h.id, h.node, h.track, h.at); err != nil {
			t.Fatal(err)
		}
	}
	hs, err := s.ListHistory("", 10)
	if err != nil || len(hs) != 3 || hs[0].ID != "h3" {
		t.Fatalf("history order wrong: %+v %v", hs, err)
	}
	if hs[1].Missing != true || hs[0].Title != "One" {
		t.Fatalf("metadata resolution wrong: %+v", hs)
	}
	hs, err = s.ListHistory("n1", 1)
	if err != nil || len(hs) != 1 || hs[0].NodeID != "n1" {
		t.Fatalf("node filter/limit wrong: %+v %v", hs, err)
	}
}
