package library

import (
	"fmt"
	"sort"
	"time"
)

type ListeningRank struct {
	Name       string `json:"name"`
	ListenMS   int64  `json:"listen_ms"`
	TrackCount int    `json:"track_count"`
}

type HeardTrack struct {
	TrackID      string `json:"track_id"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	LastPlayedAt int64  `json:"last_played_at"`
	CoverHash    string `json:"cover_hash"`
}

type HeardAlbum struct {
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	LastPlayedAt int64  `json:"last_played_at"`
	CoverHash    string `json:"cover_hash"`
	TrackCount   int    `json:"track_count"`
}

type ListeningSummary struct {
	Days            int             `json:"days"`
	From            int64           `json:"from"`
	Until           int64           `json:"until"`
	Timezone        string          `json:"timezone"`
	ListenMS        int64           `json:"listen_ms"`
	TrackCount      int             `json:"track_count"`
	AlbumCount      int             `json:"album_count"`
	ArtistCount     int             `json:"artist_count"`
	UnmeasuredPlays int             `json:"unmeasured_plays"`
	Genres          []ListeningRank `json:"genres"`
	Artists         []ListeningRank `json:"artists"`
	RecentTracks    []HeardTrack    `json:"recent_tracks"`
	RecentAlbums    []HeardAlbum    `json:"recent_albums"`
}

func (s *Service) Listening(days int, nodeID string) (ListeningSummary, error) {
	return s.listeningAt(days, nodeID, time.Now())
}

func (s *Service) listeningAt(days int, nodeID string, now time.Time) (ListeningSummary, error) {
	if days == 0 {
		days = 7
	}
	if days != 1 && days != 7 && days != 30 {
		return ListeningSummary{}, fmt.Errorf("days 必须为 1、7 或 30")
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-days)
	out := ListeningSummary{Days: days, From: start.Unix(), Until: now.Unix(), Timezone: now.Format("MST -07:00"),
		Genres: []ListeningRank{}, Artists: []ListeningRank{}, RecentTracks: []HeardTrack{}, RecentAlbums: []HeardAlbum{}}
	rows, err := s.db.Query(`SELECT h.track_id, h.title, h.measured, h.artist, h.album, h.genre, h.cover_hash,
		COALESCE(d.listen_ms,0), COALESCE(d.last_at,h.started_at)
		FROM play_history h
		LEFT JOIN (SELECT history_id,SUM(listen_ms) listen_ms,MAX(last_played_at) last_at
			FROM listen_daily WHERE day>=? AND day<=? GROUP BY history_id) d ON d.history_id=h.id
		WHERE (?='' OR h.node_id=?) AND
		(COALESCE(d.listen_ms,0)>0 OR (h.measured=0 AND h.started_at>=? AND h.started_at<=?))`,
		start.Unix(), now.Unix(), nodeID, nodeID, start.Unix(), now.Unix())
	if err != nil {
		return out, err
	}
	type rank struct {
		ms     int64
		tracks map[string]bool
	}
	type albumKey struct{ artist, album string }
	tracks := map[string]bool{}
	artists := map[string]*rank{}
	genres := map[string]*rank{}
	albums := map[albumKey]HeardAlbum{}
	recentTracks := map[string]HeardTrack{}
	albumTracks := map[albumKey]map[string]bool{}
	addRank := func(ranks map[string]*rank, name, id string, ms int64) {
		if ranks[name] == nil {
			ranks[name] = &rank{tracks: map[string]bool{}}
		}
		ranks[name].ms += ms
		ranks[name].tracks[id] = true
	}
	artistNames := map[string]bool{}
	for rows.Next() {
		var id, title, artist, album, genre, cover string
		var measured bool
		var ms, last int64
		if err := rows.Scan(&id, &title, &measured, &artist, &album, &genre, &cover, &ms, &last); err != nil {
			rows.Close()
			return out, err
		}
		tracks[id] = true
		if prev, ok := recentTracks[id]; !ok || last > prev.LastPlayedAt {
			recentTracks[id] = HeardTrack{TrackID: id, Title: title, Artist: artist, Album: album, LastPlayedAt: last, CoverHash: cover}
		}
		out.ListenMS += ms
		if !measured {
			out.UnmeasuredPlays++
		}
		key := albumKey{artist, album}
		if albumTracks[key] == nil {
			albumTracks[key] = map[string]bool{}
		}
		albumTracks[key][id] = true
		if prev, ok := albums[key]; !ok || last > prev.LastPlayedAt {
			albums[key] = HeardAlbum{Artist: artist, Album: album, LastPlayedAt: last, CoverHash: cover}
		}
		if artist == "" {
			artist = "未知艺术家"
		}
		artistNames[artist] = true
		if genre == "" {
			genre = "未分类"
		}
		if ms > 0 {
			addRank(artists, artist, id, ms)
			addRank(genres, genre, id, ms)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.TrackCount, out.AlbumCount, out.ArtistCount = len(tracks), len(albums), len(artistNames)
	makeRanks := func(in map[string]*rank) []ListeningRank {
		result := make([]ListeningRank, 0, len(in))
		for name, r := range in {
			result = append(result, ListeningRank{Name: name, ListenMS: r.ms, TrackCount: len(r.tracks)})
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].ListenMS == result[j].ListenMS {
				return result[i].Name < result[j].Name
			}
			return result[i].ListenMS > result[j].ListenMS
		})
		return result
	}
	out.Genres, out.Artists = makeRanks(genres), makeRanks(artists)
	for _, t := range recentTracks {
		out.RecentTracks = append(out.RecentTracks, t)
	}
	sort.Slice(out.RecentTracks, func(i, j int) bool {
		a, b := out.RecentTracks[i], out.RecentTracks[j]
		if a.LastPlayedAt == b.LastPlayedAt {
			return a.Title < b.Title
		}
		return a.LastPlayedAt > b.LastPlayedAt
	})
	if len(out.RecentTracks) > 100 {
		out.RecentTracks = out.RecentTracks[:100]
	}
	for key, a := range albums {
		a.TrackCount = len(albumTracks[key])
		out.RecentAlbums = append(out.RecentAlbums, a)
	}
	sort.Slice(out.RecentAlbums, func(i, j int) bool {
		a, b := out.RecentAlbums[i], out.RecentAlbums[j]
		if a.LastPlayedAt == b.LastPlayedAt {
			if a.Artist == b.Artist {
				return a.Album < b.Album
			}
			return a.Artist < b.Artist
		}
		return a.LastPlayedAt > b.LastPlayedAt
	})
	if len(out.RecentAlbums) > 50 {
		out.RecentAlbums = out.RecentAlbums[:50]
	}
	return out, nil
}
