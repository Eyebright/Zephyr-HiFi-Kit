package library

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ArtistIndexEntry is one materialized artist row. SortKey is lower-case pinyin
// so prefix search works without recomputing pinyin per request.
type ArtistIndexEntry struct {
	Name       string `json:"name"`
	SortKey    string `json:"sort_key"`
	Initial    string `json:"initial"`
	FirstChar  string `json:"first_char"`
	TrackCount int    `json:"track_count"`
}

type InitialCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type ArtistIndex struct {
	Mode     string             `json:"mode"`
	Version  int64              `json:"version"`
	Total    int                `json:"total"`
	Initials []InitialCount     `json:"initials"`
	Items    []ArtistIndexEntry `json:"items"`
}

func browseFirstChar(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "#"
	}
	r, _ := utf8.DecodeRuneInString(name)
	if unicode.IsDigit(r) {
		return "0-9"
	}
	if unicode.IsLetter(r) {
		if r < 128 {
			return strings.ToUpper(string(r))
		}
		return string(r)
	}
	return "#"
}

// EnsureArtistIndex rebuilds the materialized index when it is missing or the
// track count changed. It is cheap enough to run at startup.
func (s *Service) EnsureArtistIndex(ctx context.Context) error {
	var total, built int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&total); err != nil {
		return err
	}
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(tracks),0) FROM artist_index_meta`).Scan(&built)
	if total == 0 || built == total {
		return nil
	}
	return s.RebuildArtistIndex(ctx)
}

// RebuildArtistIndex recomputes both artist modes after a scan.
func (s *Service) RebuildArtistIndex(ctx context.Context) error {
	s.artistMu.Lock()
	defer s.artistMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rebuildArtistIndex(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func rebuildArtistIndex(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM artist_index`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO artist_index
		(mode,name,sort_key,initial,first_char,track_count,album_count,duration_ms,cover_hash)
		VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, mode := range []string{"album", "track"} {
		expr := albumArtistSQL
		if mode == "track" {
			expr = "t.artist"
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+expr+` AS name,COUNT(*),COUNT(DISTINCT t.album),
			COALESCE(SUM(t.duration_ms),0),COALESCE(MAX(NULLIF(t.cover_hash,'')),'')
			FROM tracks t GROUP BY `+expr)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var name, cover string
			var tracks, albums int
			var duration int64
			if err := rows.Scan(&name, &tracks, &albums, &duration, &cover); err != nil {
				rows.Close()
				return err
			}
			if _, err := stmt.ExecContext(ctx, mode, name, pinyinKey(name), browseInitial(name),
				browseFirstChar(name), tracks, albums, duration, cover); err != nil {
				rows.Close()
				return err
			}
			count++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if _, err := tx.ExecContext(ctx, `INSERT INTO artist_index_meta(mode,built_at,tracks) VALUES(?,?,?)
			ON CONFLICT(mode) DO UPDATE SET built_at=excluded.built_at,tracks=excluded.tracks`,
			mode, now, count); err != nil {
			return err
		}
	}
	return nil
}

// ArtistIndex returns the compact, client-cacheable artist index for one mode.
func (s *Service) ArtistIndex(ctx context.Context, mode string) (ArtistIndex, error) {
	if mode == "" {
		mode = "album"
	}
	if mode != "album" && mode != "track" {
		return ArtistIndex{}, fmt.Errorf("无效艺术家模式")
	}
	out := ArtistIndex{Mode: mode, Items: []ArtistIndexEntry{}, Initials: []InitialCount{}}
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(built_at,0) FROM artist_index_meta WHERE mode=?`, mode).Scan(&out.Version)
	rows, err := s.db.QueryContext(ctx, `SELECT name,sort_key,initial,first_char,track_count
		FROM artist_index WHERE mode=? ORDER BY sort_key,name`, mode)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var e ArtistIndexEntry
		if err := rows.Scan(&e.Name, &e.SortKey, &e.Initial, &e.FirstChar, &e.TrackCount); err != nil {
			return out, err
		}
		out.Items = append(out.Items, e)
		counts[e.Initial]++
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Total = len(out.Items)
	order := []string{}
	for c := 'A'; c <= 'Z'; c++ {
		order = append(order, string(c))
	}
	order = append(order, "0-9", "#")
	for _, key := range order {
		if counts[key] > 0 {
			out.Initials = append(out.Initials, InitialCount{Key: key, Count: counts[key]})
		}
	}
	return out, nil
}
