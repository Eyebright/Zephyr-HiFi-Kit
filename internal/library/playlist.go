package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
)

// Playlist is a user-defined, server-stored track list. Items reference library
// tracks by id without a foreign key: a rescan replaces track rows, so entries
// resolve as Missing instead of being silently deleted (same rule as the queue).
type Playlist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// PlaylistTrack is one resolved playlist entry. The JSON shape mirrors
// zatserver.QueueTrack so the console renders both identically.
type PlaylistTrack struct {
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	Decodable  bool   `json:"decodable"`
	Missing    bool   `json:"missing"`
}

// PlaylistDetail is a playlist with its ordered, resolved entries.
type PlaylistDetail struct {
	Playlist
	Items []PlaylistTrack `json:"items"`
}

// HistoryEntry is one recorded playback start (ZAT push only).
type HistoryEntry struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	StartedAt int64  `json:"started_at"`
	PlaylistTrack
}

// ListPlaylists returns all playlists, most recently touched first.
func (s *Service) ListPlaylists() ([]Playlist, error) {
	rows, err := s.db.Query(`SELECT p.id,p.name,p.created_at,p.updated_at,
		(SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id=p.id)
		FROM playlists p ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playlist{}
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePlaylist creates an empty playlist.
func (s *Service) CreatePlaylist(name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, errors.New("播放列表名称不能为空")
	}
	now := time.Now().Unix()
	p := Playlist{ID: "pl-" + uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	if _, err := s.db.Exec(`INSERT INTO playlists(id,name,created_at,updated_at) VALUES(?,?,?,?)`,
		p.ID, p.Name, now, now); err != nil {
		return Playlist{}, err
	}
	return p, nil
}

// RenamePlaylist changes a playlist's name.
func (s *Service) RenamePlaylist(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("播放列表名称不能为空")
	}
	res, err := s.db.Exec(`UPDATE playlists SET name=?,updated_at=? WHERE id=?`, name, time.Now().Unix(), id)
	return playlistResult(res, err)
}

// DeletePlaylist removes a playlist and its items.
func (s *Service) DeletePlaylist(id string) error {
	res, err := s.db.Exec(`DELETE FROM playlists WHERE id=?`, id)
	return playlistResult(res, err)
}

func playlistResult(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return catalog.ErrNotFound
	}
	return nil
}

// GetPlaylist returns one playlist with resolved items in order.
func (s *Service) GetPlaylist(id string) (PlaylistDetail, error) {
	var p Playlist
	err := s.db.QueryRow(`SELECT p.id,p.name,p.created_at,p.updated_at,
		(SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id=p.id)
		FROM playlists p WHERE p.id=?`, id).
		Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt, &p.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return PlaylistDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return PlaylistDetail{}, err
	}
	ids, err := s.playlistIDs(id)
	if err != nil {
		return PlaylistDetail{}, err
	}
	d := PlaylistDetail{Playlist: p, Items: []PlaylistTrack{}}
	for _, tid := range ids {
		d.Items = append(d.Items, s.resolveTrack(tid))
	}
	return d, nil
}

func (s *Service) playlistIDs(id string) ([]string, error) {
	rows, err := s.db.Query(`SELECT track_id FROM playlist_items WHERE playlist_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		ids = append(ids, tid)
	}
	return ids, rows.Err()
}

func (s *Service) resolveTrack(id string) PlaylistTrack {
	pt := PlaylistTrack{TrackID: id}
	if t, err := s.GetTrack(id); err == nil {
		pt.Title, pt.Artist, pt.Album = t.Title, t.Artist, t.Album
		pt.DurationMS, pt.Codec, pt.Decodable = t.DurationMS, t.Codec, t.Decodable
	} else {
		pt.Missing = true
	}
	return pt
}

// AddPlaylistItems appends tracks after validating they exist in the library.
func (s *Service) AddPlaylistItems(id string, trackIDs []string) (PlaylistDetail, error) {
	if _, err := s.GetPlaylist(id); err != nil {
		return PlaylistDetail{}, err
	}
	for _, tid := range trackIDs {
		if _, err := s.GetTrack(tid); err != nil {
			return PlaylistDetail{}, fmt.Errorf("曲目不存在：%s", tid)
		}
	}
	ids, err := s.playlistIDs(id)
	if err != nil {
		return PlaylistDetail{}, err
	}
	ids = append(ids, trackIDs...)
	if err := s.savePlaylistItems(id, ids); err != nil {
		return PlaylistDetail{}, err
	}
	return s.GetPlaylist(id)
}

// SetPlaylistItems replaces the full item order (console reorder / remove).
// Unlike AddPlaylistItems it does not require tracks to exist: reordering a
// list whose entries went Missing after a rescan must still work.
func (s *Service) SetPlaylistItems(id string, trackIDs []string) (PlaylistDetail, error) {
	if _, err := s.GetPlaylist(id); err != nil {
		return PlaylistDetail{}, err
	}
	if err := s.savePlaylistItems(id, trackIDs); err != nil {
		return PlaylistDetail{}, err
	}
	return s.GetPlaylist(id)
}

// RemovePlaylistItem drops the entry at pos and renumbers the rest.
func (s *Service) RemovePlaylistItem(id string, pos int) (PlaylistDetail, error) {
	ids, err := s.playlistIDs(id)
	if err != nil {
		return PlaylistDetail{}, err
	}
	if pos < 0 || pos >= len(ids) {
		return PlaylistDetail{}, errors.New("播放列表位置无效")
	}
	ids = append(ids[:pos], ids[pos+1:]...)
	if err := s.savePlaylistItems(id, ids); err != nil {
		return PlaylistDetail{}, err
	}
	return s.GetPlaylist(id)
}

func (s *Service) savePlaylistItems(id string, ids []string) error {
	if _, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id=?`, id); err != nil {
		return err
	}
	for i, tid := range ids {
		if _, err := s.db.Exec(`INSERT INTO playlist_items(playlist_id,position,track_id) VALUES(?,?,?)`,
			id, i, tid); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, time.Now().Unix(), id)
	return err
}

// ListHistory returns recent playback starts, newest first, with resolved
// track metadata. nodeID optionally filters to one terminal.
func (s *Service) ListHistory(nodeID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id,node_id,track_id,started_at FROM play_history`
	args := []any{}
	if nodeID != "" {
		q += ` WHERE node_id=?`
		args = append(args, nodeID)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.NodeID, &h.PlaylistTrack.TrackID, &h.StartedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Resolve after releasing the connection: the store allows only one open
	// connection, so per-row queries inside the loop would deadlock.
	for i := range out {
		out[i].PlaylistTrack = s.resolveTrack(out[i].PlaylistTrack.TrackID)
	}
	return out, nil
}
