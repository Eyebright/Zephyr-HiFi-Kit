package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"hifihub/server/internal/media"
)

var (
	ErrMetadataSourceUnavailable = errors.New("metadata source unavailable")
	ErrMetadataPreviewMismatch   = errors.New("metadata preview mismatch")
)

type MetadataOrganizationChange struct {
	ID             string `json:"id"`
	MountID        string `json:"mount_id"`
	RelPath        string `json:"rel_path"`
	Codec          string `json:"codec"`
	Container      string `json:"container"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Album          string `json:"album"`
	AlbumArtist    string `json:"album_artist"`
	NewTitle       string `json:"new_title"`
	NewArtist      string `json:"new_artist"`
	NewAlbum       string `json:"new_album"`
	NewAlbumArtist string `json:"new_album_artist"`
	NewDiscNumber  int    `json:"new_disc_number"`
	NewTrackNumber int    `json:"new_track_number"`
	Unidentified   bool   `json:"unidentified"`
}

type MetadataOrganizationIssue struct {
	ID      string `json:"id"`
	RelPath string `json:"rel_path"`
	Error   string `json:"error"`
}

type MetadataFavoriteRisk struct {
	ID          int64  `json:"id"`
	OldArtist   string `json:"old_artist"`
	OldAlbum    string `json:"old_album"`
	NewArtist   string `json:"new_artist"`
	NewAlbum    string `json:"new_album"`
	TargetCount int    `json:"target_count"`
	Action      string `json:"action"`
}

type MetadataOrganizationPreview struct {
	Count           int                          `json:"count"`
	Scanned         int                          `json:"scanned"`
	TagReads        int                          `json:"tag_reads"`
	TagErrors       int                          `json:"tag_errors"`
	Unavailable     int                          `json:"unavailable"`
	EstimatedAlbums int                          `json:"estimated_albums"`
	Unidentified    int                          `json:"unidentified"`
	Rules           map[string]int               `json:"rules"`
	Formats         map[string]int               `json:"formats"`
	FavoriteRisks   []MetadataFavoriteRisk       `json:"favorite_risks"`
	Issues          []MetadataOrganizationIssue  `json:"issues"`
	Samples         []MetadataOrganizationChange `json:"samples"`
	PreviewToken    string                       `json:"preview_token"`
}

type MetadataOrganizationResult struct {
	Updated          int `json:"updated"`
	Skipped          int `json:"skipped"`
	FavoriteMigrated int `json:"favorite_migrated"`
	FavoriteMerged   int `json:"favorite_merged"`
	FavoriteKept     int `json:"favorite_kept"`
	EstimatedAlbums  int `json:"estimated_albums"`
	Unidentified     int `json:"unidentified"`
}

type metadataAlbumKey struct {
	artist string
	album  string
}

type metadataOrganizationUpdate struct {
	change MetadataOrganizationChange
	sets   []string
	args   []any
	guards []string
}

type metadataFavoriteMove struct {
	id     int64
	source metadataAlbumKey
	target metadataAlbumKey
	merge  bool
}

type metadataFavoriteIdentity struct {
	id     int64
	artist string
	album  string
}

func (f metadataFavoriteIdentity) key() metadataAlbumKey {
	return metadataAlbumKey{artist: f.artist, album: f.album}
}

type metadataFavoriteReader interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

type metadataOrganizationPlan struct {
	preview   MetadataOrganizationPreview
	updates   []metadataOrganizationUpdate
	moves     []metadataFavoriteMove
	favorites []metadataFavoriteIdentity
	issues    []MetadataOrganizationIssue
}

type metadataOrganizationCandidateResult struct {
	next     Track
	tagFill  metadataFill
	pathFill metadataFill
	issue    error
	tagRead  bool
	tagError bool
}

const metadataOrganizationWorkers = 8

func (s *Service) PreviewMetadataOrganization(ctx context.Context, sampleLimit int) (MetadataOrganizationPreview, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	plan, err := s.buildMetadataOrganizationPlan(ctx, sampleLimit)
	if err != nil {
		return MetadataOrganizationPreview{}, err
	}
	plan.preview.PreviewToken, err = metadataOrganizationToken(plan)
	if err != nil {
		return MetadataOrganizationPreview{}, err
	}
	return plan.preview, nil
}

func (s *Service) ApplyMetadataOrganization(ctx context.Context, previewToken string, skipUnavailable bool) (MetadataOrganizationResult, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	plan, err := s.buildMetadataOrganizationPlan(ctx, 20)
	if err != nil {
		return MetadataOrganizationResult{}, err
	}
	if plan.preview.Unavailable > 0 && !skipUnavailable {
		return MetadataOrganizationResult{}, fmt.Errorf("%w: %d tracks", ErrMetadataSourceUnavailable, plan.preview.Unavailable)
	}
	token, err := metadataOrganizationToken(plan)
	if err != nil {
		return MetadataOrganizationResult{}, err
	}
	if previewToken == "" || previewToken != token {
		return MetadataOrganizationResult{}, ErrMetadataPreviewMismatch
	}
	result := MetadataOrganizationResult{
		Skipped:         plan.preview.Unavailable,
		EstimatedAlbums: plan.preview.EstimatedAlbums,
		Unidentified:    plan.preview.Unidentified,
	}
	for _, risk := range plan.preview.FavoriteRisks {
		if risk.Action == "keep" {
			result.FavoriteKept++
		}
	}

	s.artistMu.Lock()
	defer s.artistMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	favorites, err := readMetadataFavorites(tx)
	if err != nil {
		return result, err
	}
	if !slices.Equal(favorites, plan.favorites) {
		return result, ErrMetadataPreviewMismatch
	}
	for _, update := range plan.updates {
		query := `UPDATE tracks SET ` + strings.Join(update.sets, ",") + ` WHERE id=? AND ` + strings.Join(update.guards, " AND ")
		args := append(append([]any{}, update.args...), update.change.ID)
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return result, err
		}
		if n != 1 {
			return result, ErrMetadataPreviewMismatch
		}
	}
	for _, move := range plan.moves {
		if !move.merge {
			continue
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM favorites WHERE id=? AND artist=? AND album=?`, move.id, move.source.artist, move.source.album)
		if err != nil {
			return result, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return result, err
		}
		if n != 1 {
			return result, ErrMetadataPreviewMismatch
		}
		result.FavoriteMerged++
	}
	pending := make([]bool, len(plan.moves))
	migrating := map[metadataAlbumKey]int{}
	for i, move := range plan.moves {
		pending[i] = !move.merge
		if pending[i] {
			migrating[move.source] = i
		}
	}
	for remaining := len(migrating); remaining > 0; {
		progress := false
		for i, move := range plan.moves {
			if !pending[i] {
				continue
			}
			if blocker, ok := migrating[move.target]; ok && pending[blocker] {
				continue
			}
			res, err := tx.ExecContext(ctx, `UPDATE favorites SET artist=?,album=?,title=? WHERE id=? AND artist=? AND album=?`,
				move.target.artist, move.target.album, move.target.album, move.id, move.source.artist, move.source.album)
			if err != nil {
				return result, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return result, err
			}
			if n != 1 {
				return result, ErrMetadataPreviewMismatch
			}
			pending[i] = false
			remaining--
			progress = true
			result.FavoriteMigrated++
		}
		if !progress {
			return result, ErrMetadataPreviewMismatch
		}
	}
	if err := rebuildArtistIndex(ctx, tx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.Updated = len(plan.updates)
	return result, nil
}

func (s *Service) buildMetadataOrganizationPlan(ctx context.Context, sampleLimit int) (metadataOrganizationPlan, error) {
	if sampleLimit <= 0 {
		sampleLimit = 20
	}
	if sampleLimit > 100 {
		sampleLimit = 100
	}
	plan := metadataOrganizationPlan{preview: MetadataOrganizationPreview{
		Rules:         map[string]int{},
		Formats:       map[string]int{},
		FavoriteRisks: []MetadataFavoriteRisk{},
		Issues:        []MetadataOrganizationIssue{},
		Samples:       []MetadataOrganizationChange{},
	}}
	tracks, err := s.ListTracks("")
	if err != nil {
		return plan, err
	}
	remaining := map[metadataAlbumKey]int{}
	for _, tr := range tracks {
		remaining[trackAlbumKey(tr)]++
	}
	resolver := newMetadataPathResolver(tracks)
	mounts, err := s.ListMounts()
	if err != nil {
		return plan, err
	}
	mountsByID := make(map[string]Mount, len(mounts))
	for _, m := range mounts {
		mountsByID[m.ID] = m
	}
	candidates := make([]Track, 0, len(tracks))
	for _, tr := range tracks {
		if metadataOrganizationNeeded(tr) {
			candidates = append(candidates, tr)
		}
	}
	results := make([]metadataOrganizationCandidateResult, len(candidates))
	jobs := make(chan int)
	workers := min(metadataOrganizationWorkers, len(candidates))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					results[i].issue = err
					continue
				}
				results[i] = s.previewMetadataOrganizationCandidate(candidates[i], resolver, mountsByID)
			}
		}()
	}
send:
	for i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return plan, err
	}

	plan.preview.Scanned = len(candidates)
	targets := map[metadataAlbumKey]map[metadataAlbumKey]struct{}{}
	albums := map[metadataAlbumKey]struct{}{}
	for i, tr := range candidates {
		result := results[i]
		if result.issue != nil {
			plan.addIssue(tr, result.issue, sampleLimit)
			continue
		}
		if result.tagRead {
			plan.preview.TagReads++
		}
		if result.tagError {
			plan.preview.TagErrors++
		}
		next := result.next
		update, changed := metadataUpdateFor(tr, next)
		if !changed {
			continue
		}
		plan.preview.Count++
		plan.preview.Rules = addMetadataRules(plan.preview.Rules, result.tagFill, result.pathFill)
		format := tr.Codec + "/" + tr.Container
		plan.preview.Formats[format]++
		if strings.HasPrefix(next.Album, unidentifiedAlbumPrefix) {
			plan.preview.Unidentified++
		}
		newKey := trackAlbumKey(next)
		albums[newKey] = struct{}{}
		oldKey := trackAlbumKey(tr)
		if oldKey != newKey {
			remaining[oldKey]--
			if targets[oldKey] == nil {
				targets[oldKey] = map[metadataAlbumKey]struct{}{}
			}
			targets[oldKey][newKey] = struct{}{}
		}
		if len(plan.preview.Samples) < sampleLimit {
			plan.preview.Samples = append(plan.preview.Samples, update.change)
		}
		plan.updates = append(plan.updates, update)
	}
	plan.preview.EstimatedAlbums = len(albums)
	plan.favorites, err = readMetadataFavorites(s.db)
	if err != nil {
		return plan, err
	}
	plan.preview.FavoriteRisks, plan.moves = metadataFavoriteRisks(targets, remaining, plan.favorites)
	return plan, nil
}

func (s *Service) previewMetadataOrganizationCandidate(tr Track, resolver *metadataPathResolver, mounts map[string]Mount) metadataOrganizationCandidateResult {
	result := metadataOrganizationCandidateResult{next: tr}
	_, cue := cueAudioRel(tr.RelPath)
	if !cue || tr.CueStart == 0 {
		p, err := s.playbackPath(tr.ID)
		if err != nil {
			result.issue = err
			return result
		}
		m := mounts[tr.MountID]
		tags, tagErr := media.ReadTagsWithEncoding(p, m.TextEncoding)
		result.tagRead = true
		if tagErr != nil {
			var pathErr *os.PathError
			if errors.As(tagErr, &pathErr) {
				result.issue = tagErr
				return result
			}
			result.tagError = true
		}
		result.tagFill = fillTrackFromTags(&result.next, tags)
	}
	result.pathFill = resolver.apply(&result.next)
	return result
}

func metadataOrganizationNeeded(tr Track) bool {
	return tr.Title == "" || tr.Artist == "" || tr.Album == "" || tr.AlbumArtist == "" || tr.DiscNumber == 0 || tr.TrackNumber == 0
}

func (p *metadataOrganizationPlan) addIssue(tr Track, err error, limit int) {
	p.preview.Unavailable++
	issue := MetadataOrganizationIssue{ID: tr.ID, RelPath: tr.RelPath, Error: err.Error()}
	p.issues = append(p.issues, issue)
	if len(p.preview.Issues) < limit {
		p.preview.Issues = append(p.preview.Issues, issue)
	}
}

func fillTrackFromTags(tr *Track, tags media.Tags) metadataFill {
	var fill metadataFill
	if tr.Title == "" && tags.Title != "" && !damagedTagText(tags.Title) {
		tr.Title = tags.Title
		fill.Title = true
	}
	if tr.Artist == "" && tags.Artist != "" && !damagedTagText(tags.Artist) {
		tr.Artist = tags.Artist
		fill.Artist = true
	}
	if tr.Album == "" && tags.Album != "" && !damagedTagText(tags.Album) {
		tr.Album = tags.Album
		fill.Album = true
	}
	if tr.AlbumArtist == "" && tags.AlbumArtist != "" && !damagedTagText(tags.AlbumArtist) {
		tr.AlbumArtist = tags.AlbumArtist
		fill.AlbumArtist = true
	}
	if tr.DiscNumber == 0 && tags.DiscNumber > 0 {
		tr.DiscNumber = tags.DiscNumber
		fill.DiscNumber = true
	}
	if tr.TrackNumber == 0 && tags.TrackNumber > 0 {
		tr.TrackNumber = tags.TrackNumber
		fill.TrackNumber = true
	}
	return fill
}

func addMetadataRules(rules map[string]int, tags, pathFill metadataFill) map[string]int {
	for name, filled := range map[string]bool{
		"title": tags.Title, "artist": tags.Artist, "album": tags.Album,
		"album_artist": tags.AlbumArtist, "disc_number": tags.DiscNumber, "track_number": tags.TrackNumber,
	} {
		if filled {
			rules["tag_"+name]++
		}
	}
	for name, filled := range map[string]bool{
		"title": pathFill.Title, "artist": pathFill.Artist, "album_artist": pathFill.AlbumArtist,
		"disc_number": pathFill.DiscNumber, "track_number": pathFill.TrackNumber,
	} {
		if filled {
			rules["path_"+name]++
		}
	}
	if pathFill.Album {
		if pathFill.Unidentified {
			rules["unidentified_album"]++
		} else {
			rules["path_album"]++
		}
	}
	return rules
}

func metadataUpdateFor(before, after Track) (metadataOrganizationUpdate, bool) {
	update := metadataOrganizationUpdate{change: MetadataOrganizationChange{
		ID: before.ID, MountID: before.MountID, RelPath: before.RelPath, Codec: before.Codec, Container: before.Container,
		Title: before.Title, Artist: before.Artist, Album: before.Album, AlbumArtist: before.AlbumArtist,
		NewTitle: after.Title, NewArtist: after.Artist, NewAlbum: after.Album, NewAlbumArtist: after.AlbumArtist,
		NewDiscNumber: after.DiscNumber, NewTrackNumber: after.TrackNumber,
		Unidentified: strings.HasPrefix(after.Album, unidentifiedAlbumPrefix),
	}}
	add := func(column, guard string, value any, changed bool) {
		if changed {
			update.sets = append(update.sets, column+"=?")
			update.args = append(update.args, value)
			update.guards = append(update.guards, guard)
		}
	}
	add("title", "title=''", after.Title, after.Title != before.Title)
	add("artist", "artist=''", after.Artist, after.Artist != before.Artist)
	add("album", "album=''", after.Album, after.Album != before.Album)
	add("album_artist", "album_artist=''", after.AlbumArtist, after.AlbumArtist != before.AlbumArtist)
	add("disc_number", "disc_number=0", after.DiscNumber, after.DiscNumber != before.DiscNumber)
	add("track_number", "track_number=0", after.TrackNumber, after.TrackNumber != before.TrackNumber)
	return update, len(update.sets) > 0
}

func trackAlbumKey(tr Track) metadataAlbumKey {
	artist := tr.AlbumArtist
	if artist == "" {
		artist = tr.Artist
	}
	return metadataAlbumKey{artist: artist, album: tr.Album}
}

func readMetadataFavorites(q metadataFavoriteReader) ([]metadataFavoriteIdentity, error) {
	rows, err := q.Query(`SELECT id,artist,album FROM favorites WHERE kind='album' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	favorites := []metadataFavoriteIdentity{}
	for rows.Next() {
		var f metadataFavoriteIdentity
		if err := rows.Scan(&f.id, &f.artist, &f.album); err != nil {
			return nil, err
		}
		favorites = append(favorites, f)
	}
	return favorites, rows.Err()
}

func metadataFavoriteRisks(targets map[metadataAlbumKey]map[metadataAlbumKey]struct{}, remaining map[metadataAlbumKey]int, favorites []metadataFavoriteIdentity) ([]MetadataFavoriteRisk, []metadataFavoriteMove) {
	risks := []MetadataFavoriteRisk{}
	moves := []metadataFavoriteMove{}
	if len(targets) == 0 {
		return risks, moves
	}
	byKey := make(map[metadataAlbumKey]metadataFavoriteIdentity, len(favorites))
	for _, f := range favorites {
		byKey[f.key()] = f
	}
	dest := map[metadataAlbumKey]metadataAlbumKey{}
	targetCounts := map[metadataAlbumKey]int{}
	for old, newTargets := range targets {
		keys := make([]metadataAlbumKey, 0, len(newTargets)+1)
		for key := range newTargets {
			keys = append(keys, key)
		}
		if remaining[old] > 0 {
			keys = append(keys, old)
		}
		sortMetadataAlbumKeys(keys)
		targetCounts[old] = len(keys)
		if len(keys) == 1 && keys[0] != old {
			dest[old] = keys[0]
		} else {
			dest[old] = old
		}
	}

	cycles := map[metadataAlbumKey]bool{}
	state := map[metadataAlbumKey]bool{}
	for start := range byKey {
		if state[start] {
			continue
		}
		path := []metadataAlbumKey{}
		positions := map[metadataAlbumKey]int{}
		cur := start
		for {
			if state[cur] {
				break
			}
			if i, ok := positions[cur]; ok {
				for _, key := range path[i:] {
					cycles[key] = true
				}
				break
			}
			next, ok := dest[cur]
			if !ok {
				break
			}
			if _, ok := byKey[next]; !ok {
				break
			}
			positions[cur] = len(path)
			path = append(path, cur)
			cur = next
		}
		for _, key := range path {
			state[key] = true
		}
	}

	final := make(map[metadataAlbumKey]metadataAlbumKey, len(favorites))
	for _, f := range favorites {
		old := f.key()
		cur := old
		for {
			if cycles[cur] {
				final[old] = cur
				break
			}
			next, ok := dest[cur]
			if !ok {
				final[old] = cur
				break
			}
			if _, ok := byKey[next]; !ok {
				final[old] = next
				break
			}
			cur = next
		}
	}
	groups := map[metadataAlbumKey][]metadataFavoriteIdentity{}
	for _, f := range favorites {
		key := final[f.key()]
		groups[key] = append(groups[key], f)
	}
	for key, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			iStable, jStable := group[i].key() == key, group[j].key() == key
			if iStable != jStable {
				return iStable
			}
			return group[i].id < group[j].id
		})
		survivor := group[0]
		for _, f := range group {
			old := f.key()
			action := ""
			switch {
			case f.id != survivor.id:
				action = "merge"
			case old != key:
				action = "migrate"
			case targetCounts[old] > 0:
				action = "keep"
			}
			if action == "" {
				continue
			}
			risk := MetadataFavoriteRisk{ID: f.id, OldArtist: f.artist, OldAlbum: f.album, TargetCount: targetCounts[old], Action: action}
			if action != "keep" {
				risk.NewArtist, risk.NewAlbum = key.artist, key.album
				moves = append(moves, metadataFavoriteMove{id: f.id, source: old, target: key, merge: action == "merge"})
			}
			risks = append(risks, risk)
		}
	}
	sort.Slice(risks, func(i, j int) bool { return risks[i].ID < risks[j].ID })
	sort.Slice(moves, func(i, j int) bool { return moves[i].id < moves[j].id })
	return risks, moves
}

func sortMetadataAlbumKeys(keys []metadataAlbumKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].artist == keys[j].artist {
			return keys[i].album < keys[j].album
		}
		return keys[i].artist < keys[j].artist
	})
}

func metadataOrganizationToken(plan metadataOrganizationPlan) (string, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	if err := enc.Encode("zephyr-metadata-organization-v2"); err != nil {
		return "", err
	}
	for _, update := range plan.updates {
		for _, value := range []any{update.change, update.sets, update.args, update.guards} {
			if err := enc.Encode(value); err != nil {
				return "", err
			}
		}
	}
	for _, move := range plan.moves {
		for _, value := range []any{move.id, move.source.artist, move.source.album, move.target.artist, move.target.album, move.merge} {
			if err := enc.Encode(value); err != nil {
				return "", err
			}
		}
	}
	for _, issue := range plan.issues {
		for _, value := range []any{issue.ID, issue.RelPath, issue.Error} {
			if err := enc.Encode(value); err != nil {
				return "", err
			}
		}
	}
	for _, f := range plan.favorites {
		for _, value := range []any{f.id, f.artist, f.album} {
			if err := enc.Encode(value); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
