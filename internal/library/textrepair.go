package library

import (
	"context"
	"database/sql"
	"path"
	"strings"
)

// damagedTagText reports tag text whose characters were replaced by '?' (or
// U+FFFD) when the file was written by a lossy tagger. Mixed Latin+CJK titles
// keep most ASCII runes, so a run of two marks counts even when the ratio is
// low; a lone question mark in normal prose stays untouched.
func damagedTagText(s string) bool {
	runes, bad, run, maxRun := 0, 0, 0, 0
	for _, r := range s {
		runes++
		if r == '?' || r == '\uFFFD' {
			bad++
			run++
			if run > maxRun {
				maxRun = run
			}
			continue
		}
		run = 0
	}
	return runes > 0 && (maxRun >= 2 || (bad > 0 && bad*100 >= runes*40))
}

// stripTrackPrefix removes leading "01 ", "01. ", "01 - ", "01_" numbering.
func stripTrackPrefix(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i > 3 || i == len(s) {
		return s
	}
	switch s[i] {
	case ' ', '.', '-', '_':
	default:
		return s
	}
	rest := strings.TrimLeft(s[i:], " .-_")
	if rest == "" {
		return s
	}
	return rest
}

func titleFromRel(rel string) string {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	return strings.TrimSpace(stripTrackPrefix(base))
}

func artistFromRel(rel string) string {
	base := titleFromRel(rel)
	if i := strings.Index(base, " - "); i > 0 {
		return strings.TrimSpace(base[:i])
	}
	return ""
}

func albumFromRel(rel string) string {
	dir := path.Dir(rel)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return path.Base(dir)
}

// repairTrackText fills lossy '?' tags from the file name and parent folder.
// Valid tags are never modified.
func repairTrackText(title, artist, album, albumArtist, rel string) (string, string, string, string) {
	if damagedTagText(title) {
		if v := titleFromRel(rel); v != "" && !damagedTagText(v) {
			title = v
		}
	}
	if damagedTagText(artist) {
		if v := artistFromRel(rel); v != "" && !damagedTagText(v) {
			artist = v
		} else {
			artist = ""
		}
	}
	if damagedTagText(album) {
		if v := albumFromRel(rel); v != "" {
			album = v
		}
	}
	if damagedTagText(albumArtist) {
		if artist != "" && !damagedTagText(artist) {
			albumArtist = artist
		} else if albumArtist != "" {
			albumArtist = ""
		}
	}
	return title, artist, album, albumArtist
}

// TextRepairChange is one row the repair would rewrite.
type TextRepairChange struct {
	ID        string `json:"id"`
	RelPath   string `json:"rel_path"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	NewTitle  string `json:"new_title"`
	NewArtist string `json:"new_artist"`
	NewAlbum  string `json:"new_album"`
}

// TextRepairPreview reports how many tracks need the fallback repair.
type TextRepairPreview struct {
	Count   int                `json:"count"`
	Samples []TextRepairChange `json:"samples"`
}

// PreviewTextRepair lists damaged rows without writing anything.
func (s *Service) PreviewTextRepair(sampleLimit int) (TextRepairPreview, error) {
	if sampleLimit <= 0 {
		sampleLimit = 20
	}
	preview := TextRepairPreview{Samples: []TextRepairChange{}}
	rows, err := s.db.Query(`SELECT id,rel_path,title,artist,album,album_artist FROM tracks`)
	if err != nil {
		return preview, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, rel, title, artist, album, albumArtist string
		if err := rows.Scan(&id, &rel, &title, &artist, &album, &albumArtist); err != nil {
			return preview, err
		}
		nt, na, nal, naa := repairTrackText(title, artist, album, albumArtist, rel)
		if nt == title && na == artist && nal == album && naa == albumArtist {
			continue
		}
		preview.Count++
		if len(preview.Samples) < sampleLimit {
			preview.Samples = append(preview.Samples, TextRepairChange{
				ID: id, RelPath: rel, Title: title, Artist: artist, Album: album,
				NewTitle: nt, NewArtist: na, NewAlbum: nal,
			})
		}
	}
	return preview, rows.Err()
}

// repairStoredTags rewrites damaged rows in one transaction; used by scans and
// the one-time startup backfill.
func (s *Service) repairStoredTags(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,rel_path,title,artist,album,album_artist FROM tracks`)
	if err != nil {
		return 0, err
	}
	type upd struct{ id, t, a, al, aa string }
	ups := []upd{}
	for rows.Next() {
		var id, rel, title, artist, album, albumArtist string
		if err := rows.Scan(&id, &rel, &title, &artist, &album, &albumArtist); err != nil {
			rows.Close()
			return 0, err
		}
		nt, na, nal, naa := repairTrackText(title, artist, album, albumArtist, rel)
		if nt != title || na != artist || nal != album || naa != albumArtist {
			ups = append(ups, upd{id, nt, na, nal, naa})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, u := range ups {
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET title=?,artist=?,album=?,album_artist=? WHERE id=?`, u.t, u.a, u.al, u.aa, u.id); err != nil {
			return 0, err
		}
	}
	return len(ups), nil
}

// ApplyTextRepair repairs every damaged row and refreshes the artist index.
func (s *Service) ApplyTextRepair(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := s.repairStoredTags(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if n > 0 {
		if err := s.RebuildArtistIndex(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}
