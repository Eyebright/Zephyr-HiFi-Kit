package library

import "encoding/json"

// RelatedAlbum is one album suggestion with the score that placed it there.
type RelatedAlbum struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	Year       int    `json:"year"`
	Genre      string `json:"genre,omitempty"`
	CoverHash  string `json:"cover_hash"`
	TrackCount int    `json:"track_count"`
	DurationMS int64  `json:"duration_ms"`
	Favorite   bool   `json:"favorite"`
	Score      int    `json:"score"`
}

// relatedAlbumsSQL scores sibling albums: same album artist 4, shared genre 2
// (genres are multi-valued strings), year within one year 1. Targets are
// either one album or every album of an artist.
func relatedAlbumsSQL(byAlbum bool) string {
	cond := "artist=?"
	if byAlbum {
		cond = "artist=? AND album=?"
	}
	sharedGenre := `a.genre<>'' AND t.genre<>'' AND EXISTS(
		SELECT 1 FROM json_each(library_values(a.genre)) g
		WHERE g.value IN (SELECT value FROM json_each(library_values(t.genre))))`
	return `WITH albums AS (
	SELECT COALESCE(NULLIF(album_artist,''),artist) AS artist,album,
		COALESCE(MAX(year),0) AS year,
		COALESCE(MAX(NULLIF(genre,'')),'') AS genre,
		COALESCE(MAX(NULLIF(cover_hash,'')),'') AS cover_hash,
		COUNT(*) AS track_count,
		COALESCE(SUM(duration_ms),0) AS duration_ms,
		COALESCE(MAX(added_at),0) AS added_at
	FROM tracks WHERE album<>'' GROUP BY 1,2
), targets AS (SELECT artist,album,year,genre FROM albums WHERE ` + cond + `), scored AS (
	SELECT a.artist,a.album,a.year,a.genre,a.cover_hash,a.track_count,a.duration_ms,
		t.artist AS target_artist,t.album AS target_album,
		(CASE WHEN a.artist=t.artist THEN 4 ELSE 0 END
		+ CASE WHEN ` + sharedGenre + ` THEN 2 ELSE 0 END
		+ CASE WHEN a.year>0 AND t.year>0 AND abs(a.year-t.year)<=1 THEN 1 ELSE 0 END) AS score,
		a.added_at AS added_at
	FROM albums a JOIN targets t ON NOT (a.artist=t.artist AND a.album=t.album)
	WHERE a.artist=t.artist OR (` + sharedGenre + `) OR (a.year>0 AND t.year>0 AND abs(a.year-t.year)<=1)
), ranked AS (
	SELECT scored.*,ROW_NUMBER() OVER (
		PARTITION BY target_artist,target_album ORDER BY score DESC,added_at DESC,artist,album) AS rn
	FROM scored
)
SELECT target_album,artist,album,year,genre,cover_hash,track_count,duration_ms,score,
	EXISTS(SELECT 1 FROM favorites f WHERE f.kind='album' AND f.artist=ranked.artist AND f.album=ranked.album)
FROM ranked WHERE rn<=? ORDER BY target_album,rn`
}

func (s *Service) relatedScan(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]string, []RelatedAlbum, error) {
	var albums []string
	items := []RelatedAlbum{}
	for rows.Next() {
		var r RelatedAlbum
		var target string
		if err := rows.Scan(&target, &r.Artist, &r.Album, &r.Year, &r.Genre, &r.CoverHash, &r.TrackCount, &r.DurationMS, &r.Score, &r.Favorite); err != nil {
			return nil, nil, err
		}
		r.Kind, r.Name, r.Value = "album", r.Album, r.Album
		albums = append(albums, target)
		items = append(items, r)
	}
	return albums, items, rows.Err()
}

// RelatedAlbums returns up to limit albums similar to one album.
func (s *Service) RelatedAlbums(artist, album string, limit int) ([]RelatedAlbum, error) {
	limit = relatedLimit(limit)
	rows, err := s.db.Query(relatedAlbumsSQL(true), artist, album, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	_, items, err := s.relatedScan(rows)
	return items, err
}

// RelatedAlbumsByArtist returns related albums for each album of one artist in
// a single query, so an artist page never issues one request per album.
func (s *Service) RelatedAlbumsByArtist(artist string, limit int) (map[string][]RelatedAlbum, error) {
	limit = relatedLimit(limit)
	rows, err := s.db.Query(relatedAlbumsSQL(false), artist, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets, items, err := s.relatedScan(rows)
	if err != nil {
		return nil, err
	}
	groups := map[string][]RelatedAlbum{}
	for i, target := range targets {
		groups[target] = append(groups[target], items[i])
	}
	return groups, nil
}

func relatedLimit(n int) int {
	if n <= 0 {
		return 6
	}
	if n > 20 {
		return 20
	}
	return n
}

// AlbumPair is one (artist, album) target for batch related lookups.
type AlbumPair struct {
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// RelatedGroup carries the suggestions for one requested album.
type RelatedGroup struct {
	Artist string         `json:"artist"`
	Album  string         `json:"album"`
	Items  []RelatedAlbum `json:"items"`
}

// relatedPairsSQL is relatedAlbumsSQL with targets supplied as a JSON array of
// {artist,album} objects, so one list page needs a single request.
const relatedPairsSQL = `WITH albums AS (
	SELECT COALESCE(NULLIF(album_artist,''),artist) AS artist,album,
		COALESCE(MAX(year),0) AS year,
		COALESCE(MAX(NULLIF(genre,'')),'') AS genre,
		COALESCE(MAX(NULLIF(cover_hash,'')),'') AS cover_hash,
		COUNT(*) AS track_count,
		COALESCE(SUM(duration_ms),0) AS duration_ms,
		COALESCE(MAX(added_at),0) AS added_at
	FROM tracks WHERE album<>'' GROUP BY 1,2
), pairs AS (
	SELECT json_extract(value,'$.artist') AS artist,json_extract(value,'$.album') AS album
	FROM json_each(?)
), targets AS (
	SELECT a.artist,a.album,a.year,a.genre FROM albums a JOIN pairs p ON p.artist=a.artist AND p.album=a.album
), scored AS (
	SELECT a.artist,a.album,a.year,a.genre,a.cover_hash,a.track_count,a.duration_ms,
		t.artist AS target_artist,t.album AS target_album,
		(CASE WHEN a.artist=t.artist THEN 4 ELSE 0 END
		+ CASE WHEN a.genre<>'' AND t.genre<>'' AND EXISTS(
			SELECT 1 FROM json_each(library_values(a.genre)) g
			WHERE g.value IN (SELECT value FROM json_each(library_values(t.genre)))) THEN 2 ELSE 0 END
		+ CASE WHEN a.year>0 AND t.year>0 AND abs(a.year-t.year)<=1 THEN 1 ELSE 0 END) AS score,
		a.added_at AS added_at
	FROM albums a JOIN targets t ON NOT (a.artist=t.artist AND a.album=t.album)
	WHERE a.artist=t.artist OR (a.genre<>'' AND t.genre<>'' AND EXISTS(
		SELECT 1 FROM json_each(library_values(a.genre)) g
		WHERE g.value IN (SELECT value FROM json_each(library_values(t.genre)))))
		OR (a.year>0 AND t.year>0 AND abs(a.year-t.year)<=1)
), ranked AS (
	SELECT scored.*,ROW_NUMBER() OVER (
		PARTITION BY target_artist,target_album ORDER BY score DESC,added_at DESC,artist,album) AS rn
	FROM scored
)
SELECT target_artist,target_album,artist,album,year,genre,cover_hash,track_count,duration_ms,score,
	EXISTS(SELECT 1 FROM favorites f WHERE f.kind='album' AND f.artist=ranked.artist AND f.album=ranked.album)
FROM ranked WHERE rn<=? ORDER BY target_artist,target_album,rn`

// RelatedAlbumsForPairs answers suggestions for many albums at once.
func (s *Service) RelatedAlbumsForPairs(pairs []AlbumPair, limit int) ([]RelatedGroup, error) {
	limit = relatedLimit(limit)
	payload, err := json.Marshal(pairs)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(relatedPairsSQL, string(payload), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct{ artist, album string }
	order := []key{}
	byKey := map[key]*RelatedGroup{}
	for rows.Next() {
		var targetArtist, targetAlbum string
		var r RelatedAlbum
		if err := rows.Scan(&targetArtist, &targetAlbum, &r.Artist, &r.Album, &r.Year, &r.Genre, &r.CoverHash, &r.TrackCount, &r.DurationMS, &r.Score, &r.Favorite); err != nil {
			return nil, err
		}
		r.Kind, r.Name, r.Value = "album", r.Album, r.Album
		k := key{targetArtist, targetAlbum}
		g := byKey[k]
		if g == nil {
			g = &RelatedGroup{Artist: targetArtist, Album: targetAlbum}
			byKey[k] = g
			order = append(order, k)
		}
		g.Items = append(g.Items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	groups := make([]RelatedGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, *byKey[k])
	}
	return groups, nil
}
