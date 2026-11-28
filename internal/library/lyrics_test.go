package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLyricsText(t *testing.T) {
	raw := "[ti:标题]\n[offset:+500]\n[00:01.00][00:03.50]你好\n[00:02.00]<00:02.00>世界<00:02.50>\n_plain_"
	lines, synced := parseLyricsText(raw)
	if !synced || len(lines) != 3 {
		t.Fatalf("lines = %+v synced=%v", lines, synced)
	}
	if lines[0].TimeMS != 1500 || lines[0].Text != "你好" || lines[1].TimeMS != 2500 || lines[1].Text != "世界" || lines[2].TimeMS != 4000 {
		t.Fatalf("parsed lines = %+v", lines)
	}
	plain, synced := parseLyricsText("第一行\n第二行")
	if synced || len(plain) != 2 || plain[0].Text != "第一行" {
		t.Fatalf("plain lyrics = %+v synced=%v", plain, synced)
	}
}

func TestParseLyricsStripsInlineTimestamps(t *testing.T) {
	// Enhanced LRC with square-bracket word tags between syllables must not
	// leak timestamps into the display text.
	raw := "[00:18.70]我剑[00:19.20]何去何从[00:20.10]\n[00:21.00]爱与恨 <00:21.50>情难独钟"
	lines, synced := parseLyricsText(raw)
	if !synced || len(lines) != 2 {
		t.Fatalf("lines = %+v synced=%v", lines, synced)
	}
	if lines[0].Text != "我剑何去何从" || lines[1].Text != "爱与恨 情难独钟" {
		t.Fatalf("timestamp leaked: %+v", lines)
	}
	// A purely plain line carrying an inline tag is cleaned too.
	plain, _ := parseLyricsText("尾句 [00:30.00] 内容")
	if len(plain) != 1 || plain[0].Text != "尾句 内容" {
		t.Fatalf("plain inline tag not stripped: %+v", plain)
	}
}

func TestGetLyricsSidecarCacheAndRefresh(t *testing.T) {
	root := t.TempDir()
	s, _ := newService(t, &fakeRunner{}, root)
	m := insertMounted(t, s, root)
	audio := filepath.Join(root, "song.wav")
	buildWAV(t, audio)
	lrc := filepath.Join(root, "song.lrc")
	if err := os.WriteFile(lrc, []byte("[00:01.00]你好"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	id := tracks[0].ID
	got, err := s.GetLyrics(context.Background(), id)
	if err != nil || got.Source != "sidecar" || !got.Synced || len(got.Lines) != 1 || got.Lines[0].Text != "你好" {
		t.Fatalf("lyrics = %+v, %v", got, err)
	}
	cached, err := s.GetLyrics(context.Background(), id)
	if err != nil || cached.UpdatedAt != got.UpdatedAt {
		t.Fatalf("cached lyrics = %+v, %v", cached, err)
	}
	if err := os.WriteFile(lrc, []byte("[00:02.00]世界"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(lrc, future, future); err != nil {
		t.Fatal(err)
	}
	refreshed, err := s.GetLyrics(context.Background(), id)
	if err != nil || refreshed.Lines[0].Text != "世界" || refreshed.Lines[0].TimeMS != 2000 {
		t.Fatalf("refreshed lyrics = %+v, %v", refreshed, err)
	}
}

func TestGetLyricsNone(t *testing.T) {
	root := t.TempDir()
	s, _ := newService(t, &fakeRunner{}, root)
	m := insertMounted(t, s, root)
	buildWAV(t, filepath.Join(root, "plain.wav"))
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	got, err := s.GetLyrics(context.Background(), tracks[0].ID)
	if err != nil || got.Source != "none" || got.Synced || len(got.Lines) != 0 {
		t.Fatalf("empty lyrics = %+v, %v", got, err)
	}
}

func TestGetLyricsNegativeCacheAndSidecarInvalidation(t *testing.T) {
	root := t.TempDir()
	s, _ := newService(t, &fakeRunner{}, root)
	m := insertMounted(t, s, root)
	buildWAV(t, filepath.Join(root, "plain.wav"))
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	id := tracks[0].ID
	first, err := s.GetLyrics(context.Background(), id)
	if err != nil || first.Source != "none" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	// The "no lyrics" outcome must be persisted so the source is not re-read.
	var source string
	if err := s.db.QueryRow(`SELECT source FROM track_lyrics WHERE track_id=?`, id).Scan(&source); err != nil || source != "none" {
		t.Fatalf("negative result not cached: source=%q err=%v", source, err)
	}
	again, err := s.GetLyrics(context.Background(), id)
	if err != nil || again.Source != "none" || again.UpdatedAt != first.UpdatedAt {
		t.Fatalf("cached none not reused: %+v, %v", again, err)
	}
	// A sidecar added later must still be discovered despite the cached none.
	if err := os.WriteFile(filepath.Join(root, "plain.lrc"), []byte("[00:01.00]你好"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLyrics(context.Background(), id)
	if err != nil || got.Source != "sidecar" || !got.Synced || len(got.Lines) != 1 {
		t.Fatalf("sidecar after cached none = %+v, %v", got, err)
	}
}
