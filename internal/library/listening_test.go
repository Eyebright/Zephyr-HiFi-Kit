package library

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestListeningSummary(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	seedTrack(t, st, m.ID, "t1", "One")
	seedTrack(t, st, m.ID, "t2", "Two")
	seedTrack(t, st, m.ID, "unheard", "Never played")
	if _, err := st.DB.Exec(`UPDATE tracks SET cover_hash='live-cover'`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	today := time.Date(2026, 11, 18, 0, 0, 0, 0, time.Local)
	add := func(id, node, track, artist, album, genre string, at time.Time, measured bool) {
		t.Helper()
		if _, err := st.DB.Exec(`INSERT INTO play_history(id,node_id,track_id,artist,album,genre,started_at,measured)
			VALUES(?,?,?,?,?,?,?,?)`, id, node, track, artist, album, genre, at.Unix(), measured); err != nil {
			t.Fatal(err)
		}
	}
	listen := func(id string, day time.Time, ms int64, last time.Time) {
		t.Helper()
		if _, err := st.DB.Exec(`INSERT INTO listen_daily(history_id,day,listen_ms,last_played_at) VALUES(?,?,?,?)`,
			id, day.Unix(), ms, last.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	add("h1", "n1", "t1", "Artist", "Album", "Jazz", today.Add(-time.Minute), true)
	listen("h1", today.AddDate(0, 0, -1), 10000, today.Add(-time.Second))
	listen("h1", today, 20000, today.Add(time.Minute))
	add("h2", "n2", "t2", "Artist", "Album", "Jazz", now.Add(-time.Hour), true)
	listen("h2", today, 30000, now.Add(-time.Minute))
	add("h3", "n1", "gone", "Other", "Lost", "", now.Add(-2*time.Hour), true)
	listen("h3", today, 40000, now.Add(-2*time.Hour))
	// A legacy snapshot frozen by migration, not dynamically read from tracks.
	add("legacy", "n1", "t1", "Artist", "Album", "Jazz", now.Add(-time.Hour), false)
	add("failed", "n1", "never-heard", "Wrong", "Wrong", "", now, true)
	add("old", "n1", "old-track", "Old", "Old", "", today.AddDate(0, 0, -31), false)
	add("old-same-album", "n1", "old-album-track", "Artist", "Album", "Jazz", today.AddDate(0, 0, -31), false)
	add("future", "n1", "future-track", "Artist", "Album", "Jazz", now.AddDate(0, 0, 1), false)
	if _, err := st.DB.Exec(`UPDATE play_history SET cover_hash='snapshot-cover' WHERE id='h2'`); err != nil {
		t.Fatal(err)
	}

	summary, err := s.listeningAt(1, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ListenMS != 90000 || summary.TrackCount != 3 || summary.AlbumCount != 2 || summary.ArtistCount != 2 || summary.UnmeasuredPlays != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if summary.From != today.Unix() || summary.Until != now.Unix() || summary.Days != 1 {
		t.Fatalf("wrong calendar range: %+v", summary)
	}
	if len(summary.Genres) != 2 || summary.Genres[0].Name != "Jazz" || summary.Genres[0].ListenMS != 50000 || summary.Genres[0].TrackCount != 2 || summary.Genres[1].Name != "未分类" {
		t.Fatalf("genre ranks=%+v", summary.Genres)
	}
	wantAlbums := []HeardAlbum{
		{Artist: "Artist", Album: "Album", LastPlayedAt: now.Add(-time.Minute).Unix(), CoverHash: "snapshot-cover", TrackCount: 2},
		{Artist: "Other", Album: "Lost", LastPlayedAt: now.Add(-2 * time.Hour).Unix(), TrackCount: 1},
	}
	if !reflect.DeepEqual(summary.RecentAlbums, wantAlbums) {
		t.Fatalf("historical albums=%+v, want %+v", summary.RecentAlbums, wantAlbums)
	}
	encoded, err := json.Marshal(summary.RecentAlbums[0])
	if err != nil {
		t.Fatal(err)
	}
	var albumJSON map[string]any
	if err := json.Unmarshal(encoded, &albumJSON); err != nil {
		t.Fatal(err)
	}
	if _, ok := albumJSON["missing"]; ok || albumJSON["track_count"] != float64(2) {
		t.Fatalf("album API must expose historical count, not live availability: %s", encoded)
	}
	filtered, err := s.listeningAt(1, "n1", now)
	if err != nil || filtered.ListenMS != 60000 || filtered.TrackCount != 2 || len(filtered.RecentAlbums) != 2 || filtered.RecentAlbums[0].TrackCount != 1 || filtered.RecentAlbums[0].CoverHash != "" {
		t.Fatalf("node filter=%+v err=%v", filtered, err)
	}
	week, err := s.listeningAt(7, "", now)
	if err != nil || week.ListenMS != 100000 {
		t.Fatalf("week=%+v err=%v", week, err)
	}
	type filter struct {
		days int
		node string
	}
	before := map[filter]ListeningSummary{}
	for _, days := range []int{1, 7, 30} {
		for _, node := range []string{"", "n1", "n2", "absent"} {
			value, err := s.listeningAt(days, node, now)
			if err != nil {
				t.Fatal(err)
			}
			before[filter{days, node}] = value
		}
	}
	for _, change := range []string{
		`UPDATE tracks SET cover_hash='retagged-cover'`,
		`UPDATE tracks SET title='Changed',artist='Changed',album='Changed',genre='Changed'`,
		`DELETE FROM tracks`,
		// Only this isolated test database loses the table; queries must still work.
		`ALTER TABLE tracks RENAME TO unavailable_tracks`,
	} {
		if _, err := st.DB.Exec(change); err != nil {
			t.Fatal(err)
		}
		for f, want := range before {
			after, err := s.listeningAt(f.days, f.node, now)
			if err != nil || !reflect.DeepEqual(after, want) {
				t.Fatalf("%s changed summary for %+v: got %+v, want %+v, err=%v", change, f, after, want, err)
			}
		}
	}
}

func TestListeningUnboundedHistoryAndEmpty(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	now := time.Now()
	empty, err := s.listeningAt(0, "", now)
	if err != nil || empty.Days != 7 || empty.Genres == nil || empty.Artists == nil || empty.RecentAlbums == nil {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	if _, err := s.listeningAt(2, "", now); err == nil {
		t.Fatal("invalid range accepted")
	}
	tx, err := st.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1005; i++ {
		if _, err := tx.Exec(`INSERT INTO play_history(id,node_id,track_id,started_at) VALUES(?,?,?,?)`,
			fmt.Sprintf("h%d", i), "n", fmt.Sprintf("t%d", i), now.Unix()); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	all, err := s.listeningAt(30, "", now)
	if err != nil || all.TrackCount != 1005 || all.UnmeasuredPlays != 1005 || all.ListenMS != 0 || len(all.Artists) != 0 {
		t.Fatalf("legacy duration or truncation: %+v err=%v", all, err)
	}
	if !reflect.DeepEqual(all.RecentAlbums, []HeardAlbum{{LastPlayedAt: now.Unix(), TrackCount: 1005}}) {
		t.Fatalf("unknown historical metadata must stay empty: %+v", all.RecentAlbums)
	}
}
