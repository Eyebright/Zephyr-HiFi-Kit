package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"hifihub/server/internal/catalog"
)

type FavoriteQuery struct {
	Kind, Q        string
	Page, PageSize int
}

type FavoritePage struct {
	Items    []Favorite `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type Favorite struct {
	Kind       string `json:"kind"`
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	CoverHash  string `json:"cover_hash"`
	DurationMS int64  `json:"duration_ms"`
	Missing    bool   `json:"missing"`
	Decodable  bool   `json:"decodable"`
	TrackCount int    `json:"track_count"`
	AddedAt    int64  `json:"added_at"`
}

type FavoriteState struct {
	Track     bool   `json:"track"`
	Album     bool   `json:"album"`
	Artist    string `json:"artist"`
	AlbumName string `json:"album_name"`
}

// Pointers distinguish an explicitly unknown (empty) identity from omitted metadata.
type FavoriteRequest struct {
	Kind    string  `json:"kind"`
	TrackID string  `json:"track_id,omitempty"`
	Artist  *string `json:"artist,omitempty"`
	Album   *string `json:"album,omitempty"`
}

func favoriteTextValid(s string, maxBytes int) bool {
	return len(s) <= maxBytes && utf8.ValidString(s) && !strings.ContainsRune(s, '\x00')
}

func normalizeFavoriteQuery(q FavoriteQuery) (FavoriteQuery, error) {
	if q.Kind == "" {
		q.Kind = "track"
	}
	if q.Kind != "track" && q.Kind != "album" {
		return q, fmt.Errorf("kind 必须为 track 或 album")
	}
	if !favoriteTextValid(q.Q, 1024) {
		return q, fmt.Errorf("搜索词必须为有效文本且不超过1024字节")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 24
	}
	if q.PageSize != 6 && q.PageSize != 24 && q.PageSize != 48 && q.PageSize != 96 {
		return q, fmt.Errorf("每页数量必须为6、24、48或96")
	}
	if q.Page < 1 || q.Page-1 > int(^uint(0)>>1)/q.PageSize {
		return q, fmt.Errorf("页码超出范围")
	}
	return q, nil
}

func (r FavoriteRequest) validate(save bool) error {
	if !favoriteTextValid(r.TrackID, 512) || (r.TrackID != "" && strings.TrimSpace(r.TrackID) == "") {
		return fmt.Errorf("无效 track_id")
	}
	for _, v := range []*string{r.Artist, r.Album} {
		if v != nil && !favoriteTextValid(*v, 4096) {
			return fmt.Errorf("专辑身份必须为有效文本且不超过4096字节")
		}
	}
	switch r.Kind {
	case "track":
		if r.TrackID != "" && r.Artist == nil && r.Album == nil {
			return nil
		}
	case "album":
		if r.TrackID == "" && r.Artist != nil && r.Album != nil {
			return nil
		}
		if save && r.TrackID != "" && r.Artist == nil && r.Album == nil {
			return nil
		}
	}
	return fmt.Errorf("收藏需要 track 和 track_id，或 album 和 artist、album；仅收藏专辑时可用 track_id 解析")
}

func (s *Service) ListFavorites(query FavoriteQuery) (FavoritePage, error) {
	q, err := normalizeFavoriteQuery(query)
	page := FavoritePage{Items: []Favorite{}, Page: q.Page, PageSize: q.PageSize}
	if err != nil {
		return page, err
	}
	resolved := `SELECT f.id,f.kind,f.track_id,COALESCE(t.title,f.title) AS title,
		COALESCE(t.artist,f.artist) AS artist,COALESCE(t.album,f.album) AS album,
		COALESCE(t.cover_hash,f.cover_hash) AS cover_hash,COALESCE(t.duration_ms,f.duration_ms) AS duration_ms,
		t.id IS NULL AS missing,COALESCE(t.decodable,0)=1 AS decodable,
		CASE WHEN t.id IS NULL THEN 0 ELSE 1 END AS track_count,f.added_at
		FROM favorites f LEFT JOIN tracks t ON t.id=f.track_id WHERE f.kind='track'`
	if q.Kind == "album" {
		resolved = `SELECT f.id,f.kind,f.track_id,f.title,f.artist,f.album,
			COALESCE(MAX(NULLIF(t.cover_hash,'')),f.cover_hash) AS cover_hash,
			COALESCE(SUM(t.duration_ms),f.duration_ms) AS duration_ms,
			COUNT(t.id)=0 AS missing,COALESCE(MAX(t.decodable=1),0) AS decodable,
			COUNT(t.id) AS track_count,f.added_at
			FROM favorites f LEFT JOIN tracks t ON ` + albumArtistSQL + `=f.artist AND t.album=f.album
			WHERE f.kind='album' GROUP BY f.id`
	}
	cte := `WITH resolved AS (` + resolved + `), matches AS (SELECT * FROM resolved`
	args := []any{}
	if q.Q != "" {
		cte += ` WHERE title LIKE ? ESCAPE '\' OR artist LIKE ? ESCAPE '\' OR album LIKE ? ESCAPE '\'`
		term := likeLiteral(q.Q)
		args = append(args, term, term, term)
	}
	cte += `)`
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(cte+` SELECT COUNT(*) FROM matches`, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := tx.Query(cte+` SELECT kind,track_id,title,artist,album,cover_hash,duration_ms,missing,decodable,track_count,added_at
		FROM matches ORDER BY added_at DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Kind, &f.TrackID, &f.Title, &f.Artist, &f.Album, &f.CoverHash, &f.DurationMS,
			&f.Missing, &f.Decodable, &f.TrackCount, &f.AddedAt); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}

// A saved snapshot cannot establish the current album identity of a missing track.
func (s *Service) GetFavoriteState(trackID string) (FavoriteState, error) {
	var state FavoriteState
	if err := (FavoriteRequest{Kind: "track", TrackID: trackID}).validate(false); err != nil {
		return state, err
	}
	err := s.db.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM favorites f WHERE f.kind='track' AND f.track_id=t.id),
		EXISTS(SELECT 1 FROM favorites f WHERE f.kind='album' AND f.artist=`+albumArtistSQL+` AND f.album=t.album),
		`+albumArtistSQL+`,t.album FROM tracks t WHERE t.id=?`, trackID).
		Scan(&state.Track, &state.Album, &state.Artist, &state.AlbumName)
	if errors.Is(err, sql.ErrNoRows) {
		err = catalog.ErrNotFound
	}
	return state, err
}

// FavoriteAlbum identifies one favorited album by its (artist, album) key.
type FavoriteAlbum struct {
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// ListFavoriteAlbums returns every favorited album key so card lists can show
// heart state without one request per album.
func (s *Service) ListFavoriteAlbums() ([]FavoriteAlbum, error) {
	rows, err := s.db.Query(`SELECT artist,album FROM favorites WHERE kind='album' ORDER BY added_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	albums := []FavoriteAlbum{}
	for rows.Next() {
		var a FavoriteAlbum
		if err := rows.Scan(&a.Artist, &a.Album); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (s *Service) SaveFavorite(req FavoriteRequest) error {
	if err := req.validate(true); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if req.Kind == "album" && req.TrackID != "" {
		var artist, album string
		err := tx.QueryRow(`SELECT `+albumArtistSQL+`,t.album FROM tracks t WHERE t.id=?`, req.TrackID).Scan(&artist, &album)
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return err
		}
		req.TrackID, req.Artist, req.Album = "", &artist, &album
	}
	where, args := favoriteIdentity(req)
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM favorites WHERE `+where+`)`, args...).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit()
	}
	var f Favorite
	var codec string
	if req.Kind == "track" {
		err = tx.QueryRow(`SELECT title,artist,album,cover_hash,duration_ms,codec,decodable=1 FROM tracks WHERE id=?`, req.TrackID).
			Scan(&f.Title, &f.Artist, &f.Album, &f.CoverHash, &f.DurationMS, &codec, &f.Decodable)
	} else {
		f.Title, f.Artist, f.Album = *req.Album, *req.Artist, *req.Album
		err = tx.QueryRow(`SELECT COUNT(*),COALESCE(MAX(NULLIF(t.cover_hash,'')),''),COALESCE(SUM(t.duration_ms),0),
			COALESCE(MAX(t.decodable=1),0) FROM tracks t WHERE `+albumArtistSQL+`=? AND t.album=?`, f.Artist, f.Album).
			Scan(&f.TrackCount, &f.CoverHash, &f.DurationMS, &f.Decodable)
		if err == nil && f.TrackCount == 0 {
			return catalog.ErrNotFound
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return catalog.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO favorites(kind,track_id,title,artist,album,cover_hash,duration_ms,codec,decodable,added_at)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, req.Kind, req.TrackID, f.Title, f.Artist, f.Album,
		f.CoverHash, f.DurationMS, codec, boolInt(f.Decodable), time.Now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func favoriteIdentity(req FavoriteRequest) (string, []any) {
	if req.Kind == "track" {
		return `kind='track' AND track_id=?`, []any{req.TrackID}
	}
	return `kind='album' AND artist=? AND album=?`, []any{*req.Artist, *req.Album}
}

func (s *Service) DeleteFavorite(req FavoriteRequest) error {
	if err := req.validate(false); err != nil {
		return err
	}
	where, args := favoriteIdentity(req)
	_, err := s.db.Exec(`DELETE FROM favorites WHERE `+where, args...)
	return err
}
