package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedOrganizationTrack(t *testing.T, s *Service, mountID, root string, tr Track) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO tracks
		(id,mount_id,path,rel_path,title,artist,album,album_artist,disc_number,track_number,cue_start,scanned_at,decodable)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, tr.ID, mountID, filepath.Join(root, filepath.FromSlash(tr.RelPath)), tr.RelPath,
		tr.Title, tr.Artist, tr.Album, tr.AlbumArtist, tr.DiscNumber, tr.TrackNumber, tr.CueStart, 1, boolInt(tr.Decodable))
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetadataOrganizationDoesNotWaitForSourceLifecycleLock(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(dir, "01 Song.wav"))
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "track", RelPath: "Artist/Album/01 Song.wav", Decodable: true})

	s.sourceMu.Lock()
	type previewResult struct {
		preview MetadataOrganizationPreview
		err     error
	}
	previewDone := make(chan previewResult, 1)
	go func() {
		preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
		previewDone <- previewResult{preview: preview, err: err}
	}()
	var result previewResult
	select {
	case result = <-previewDone:
		s.sourceMu.Unlock()
	case <-time.After(5 * time.Second):
		s.sourceMu.Unlock()
		<-previewDone
		t.Fatal("preview waited for the source lifecycle lock")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}

	s.sourceMu.Lock()
	applyDone := make(chan error, 1)
	go func() {
		_, err := s.ApplyMetadataOrganization(t.Context(), result.preview.PreviewToken, false)
		applyDone <- err
	}()
	select {
	case err := <-applyDone:
		s.sourceMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		s.sourceMu.Unlock()
		<-applyDone
		t.Fatal("apply waited for the source lifecycle lock")
	}
}

func TestMetadataOrganizationPreviewIsDeterministic(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"02 Second.wav", "01 First.wav"} {
		buildWAV(t, filepath.Join(dir, file))
	}
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "second", RelPath: "Artist/Album/02 Second.wav", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "first", RelPath: "Artist/Album/01 First.wav", Decodable: true})

	first, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.PreviewToken == "" || first.PreviewToken != second.PreviewToken {
		t.Fatalf("preview tokens differ: %q != %q", first.PreviewToken, second.PreviewToken)
	}
	if first.Count != 2 || len(first.Samples) != 2 || first.Samples[0].RelPath != "Artist/Album/01 First.wav" || first.Samples[1].RelPath != "Artist/Album/02 Second.wav" {
		t.Fatalf("preview order = %+v", first)
	}
}

func TestMetadataOrganizationPreviewAndApply(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	albumDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taggedPath := filepath.Join(albumDir, "01 Tagged.wav")
	buildWAV(t, taggedPath)
	addWAVTags(t, taggedPath, [2]string{"INAM", "Tagged title\x00"}, [2]string{"IART", "Tagged Artist\x00"}, [2]string{"IPRD", "Tagged Album\x00"})
	buildWAV(t, filepath.Join(albumDir, "02 Plain.wav"))
	buildWAV(t, filepath.Join(root, "03 Singer - Single.wav"))
	buildWAV(t, filepath.Join(albumDir, "04 Partial.wav"))
	addWAVTags(t, filepath.Join(albumDir, "04 Partial.wav"), [2]string{"IART", "Tag Other\x00"}, [2]string{"IPRD", "Other Album\x00"})

	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "tagged", RelPath: "Artist/Album/01 Tagged.wav", Title: "01 Tagged", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "plain", RelPath: "Artist/Album/02 Plain.wav", Title: "02 Plain", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "single", RelPath: "03 Singer - Single.wav", Title: "03 Singer - Single", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "partial", RelPath: "Artist/Album/04 Partial.wav", Title: "04 Partial", Artist: "Keep Artist", Decodable: true})
	empty := ""
	if err := s.SaveFavorite(FavoriteRequest{Kind: "album", Artist: &empty, Album: &empty}); err != nil {
		t.Fatal(err)
	}

	preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 4 || preview.Scanned != 4 || preview.TagReads != 4 || preview.Unavailable != 0 || preview.EstimatedAlbums != 4 || preview.Unidentified != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	if len(preview.FavoriteRisks) != 1 || preview.FavoriteRisks[0].Action != "keep" {
		t.Fatalf("favorite risks = %+v", preview.FavoriteRisks)
	}
	before, _ := s.ListTracks("")
	for _, tr := range before {
		if tr.Artist != "" && tr.ID != "partial" || tr.Album != "" {
			t.Fatalf("preview modified database: %+v", tr)
		}
	}

	result, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 4 || result.FavoriteKept != 1 || result.FavoriteMigrated != 0 || result.FavoriteMerged != 0 {
		t.Fatalf("result = %+v", result)
	}
	after, _ := s.ListTracks("")
	byID := map[string]Track{}
	for _, tr := range after {
		byID[tr.ID] = tr
	}
	if tr := byID["tagged"]; tr.Artist != "Tagged Artist" || tr.Album != "Tagged Album" {
		t.Fatalf("embedded tags not preferred: %+v", tr)
	}
	if tr := byID["plain"]; tr.Artist != "Artist" || tr.Album != "Album" || tr.AlbumArtist != "Artist" || tr.TrackNumber != 2 {
		t.Fatalf("path fallback wrong: %+v", tr)
	}
	if tr := byID["single"]; tr.Artist != "Singer" || tr.Album != unidentifiedAlbumPrefix+"Single" {
		t.Fatalf("flat single wrong: %+v", tr)
	}
	if tr := byID["partial"]; tr.Artist != "Keep Artist" || tr.Album != "Other Album" {
		t.Fatalf("valid artist overwritten: %+v", tr)
	}
	favorites, err := s.ListFavoriteAlbums()
	if err != nil || len(favorites) != 1 || favorites[0].Artist != "" || favorites[0].Album != "" {
		t.Fatalf("ambiguous favorite was not preserved: %+v %v", favorites, err)
	}
}

func TestMetadataOrganizationMigratesAndMergesFavorites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		merge bool
	}{
		{name: "migrate"},
		{name: "merge", merge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
			root := t.TempDir()
			m := insertMounted(t, s, root)
			dir := filepath.Join(root, "New Artist", "New Album")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			buildWAV(t, filepath.Join(dir, "01 Song.wav"))
			seedOrganizationTrack(t, s, m.ID, root, Track{ID: "moving", RelPath: "New Artist/New Album/01 Song.wav", Title: "01 Song", Artist: "Old Artist", Decodable: true})
			oldArtist, oldAlbum := "Old Artist", ""
			if err := s.SaveFavorite(FavoriteRequest{Kind: "album", Artist: &oldArtist, Album: &oldAlbum}); err != nil {
				t.Fatal(err)
			}
			if tc.merge {
				buildWAV(t, filepath.Join(dir, "02 Existing.wav"))
				seedOrganizationTrack(t, s, m.ID, root, Track{ID: "target", RelPath: "New Artist/New Album/02 Existing.wav", Title: "Existing", Artist: "New Artist", Album: "New Album", Decodable: true})
				newArtist, newAlbum := "New Artist", "New Album"
				if err := s.SaveFavorite(FavoriteRequest{Kind: "album", Artist: &newArtist, Album: &newAlbum}); err != nil {
					t.Fatal(err)
				}
			}
			preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.FavoriteRisks) != 1 || preview.FavoriteRisks[0].Action != map[bool]string{false: "migrate", true: "merge"}[tc.merge] {
				t.Fatalf("risk = %+v", preview.FavoriteRisks)
			}
			result, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false)
			if err != nil {
				t.Fatal(err)
			}
			if tc.merge && (result.FavoriteMerged != 1 || result.FavoriteMigrated != 0) || !tc.merge && (result.FavoriteMigrated != 1 || result.FavoriteMerged != 0) {
				t.Fatalf("result = %+v", result)
			}
			favorites, err := s.ListFavoriteAlbums()
			if err != nil || len(favorites) != 1 || favorites[0].Artist != "New Artist" || favorites[0].Album != "New Album" {
				t.Fatalf("favorite identity = %+v %v", favorites, err)
			}
		})
	}
}

func TestMetadataOrganizationUnavailableSourceBlocksUnlessExplicitlySkipped(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(dir, "01 Song.wav"))
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "available", RelPath: "Artist/Album/01 Song.wav", Title: "01 Song", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "missing", RelPath: "missing.wav", Title: "missing", Decodable: true})
	preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Unavailable != 1 || preview.Count != 1 || len(preview.Issues) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	if _, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false); !errors.Is(err, ErrMetadataSourceUnavailable) {
		t.Fatalf("apply error = %v", err)
	}
	missing, err := s.GetTrack("missing")
	if err != nil || missing.Artist != "" || missing.Album != "" {
		t.Fatalf("database changed: %+v %v", missing, err)
	}
	result, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, true)
	if err != nil || result.Updated != 1 || result.Skipped != 1 {
		t.Fatalf("partial apply = %+v, %v", result, err)
	}
	available, err := s.GetTrack("available")
	if err != nil || available.Artist != "Artist" || available.Album != "Album" {
		t.Fatalf("available track = %+v %v", available, err)
	}
	missing, err = s.GetTrack("missing")
	if err != nil || missing.Artist != "" || missing.Album != "" {
		t.Fatalf("unavailable track changed: %+v %v", missing, err)
	}
}

func TestMetadataOrganizationSkipUnavailableBindsIssueSet(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "missing", RelPath: "missing.wav", Title: "missing", Decodable: true})
	preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE tracks SET rel_path='renamed-missing.wav' WHERE id='missing'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, true); !errors.Is(err, ErrMetadataPreviewMismatch) {
		t.Fatalf("apply error = %v", err)
	}
}

func TestMetadataOrganizationFillsMissingFieldsAndRebuildsIndex(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	dir := filepath.Join(root, "Path Artist", "Path Album", "CD 2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(dir, "03 Song.wav"))
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "partial", RelPath: "Path Artist/Path Album/CD 2/03 Song.wav", Title: "Keep Title", Artist: "Keep Artist", Decodable: true})
	if err := s.RebuildArtistIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 || preview.Scanned != 1 || preview.TagReads != 1 || preview.Unavailable != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	result, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false)
	if err != nil || result.Updated != 1 {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	tr, err := s.GetTrack("partial")
	if err != nil || tr.Title != "Keep Title" || tr.Artist != "Keep Artist" || tr.Album != "Path Album" || tr.AlbumArtist != "Path Artist" || tr.DiscNumber != 2 || tr.TrackNumber != 3 {
		t.Fatalf("track = %+v, %v", tr, err)
	}
	index, err := s.ArtistIndex(t.Context(), "track")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range index.Items {
		if item.Name == "" {
			t.Fatal("stale empty artist remained in index")
		}
		if item.Name == "Keep Artist" && item.TrackCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated artist missing from index: %+v", index.Items)
	}
}

func TestMetadataOrganizationRejectsChangedPreview(t *testing.T) {
	for _, change := range []string{"track", "favorite"} {
		t.Run(change, func(t *testing.T) {
			s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
			root := t.TempDir()
			m := insertMounted(t, s, root)
			dir := filepath.Join(root, "Artist", "Album")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			buildWAV(t, filepath.Join(dir, "01 Song.wav"))
			seedOrganizationTrack(t, s, m.ID, root, Track{ID: "track", RelPath: "Artist/Album/01 Song.wav", Title: "01 Song", Decodable: true})
			seedOrganizationTrack(t, s, m.ID, root, Track{ID: "other", RelPath: "Other/Album/01 Other.wav", Title: "Other", Artist: "Other", Album: "Album", AlbumArtist: "Other", DiscNumber: 1, TrackNumber: 1, Decodable: true})
			preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if change == "track" {
				if _, err := s.db.Exec(`UPDATE tracks SET artist='Manual Artist' WHERE id='track'`); err != nil {
					t.Fatal(err)
				}
			} else {
				artist, album := "Other", "Album"
				if err := s.SaveFavorite(FavoriteRequest{Kind: "album", Artist: &artist, Album: &album}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false); !errors.Is(err, ErrMetadataPreviewMismatch) {
				t.Fatalf("apply error = %v", err)
			}
			tr, err := s.GetTrack("track")
			if err != nil || tr.Album != "" || tr.Artist != map[string]string{"track": "Manual Artist", "favorite": ""}[change] {
				t.Fatalf("database changed: %+v %v", tr, err)
			}
		})
	}
}

func TestMetadataOrganizationMigratesMultipleFavorites(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	root := t.TempDir()
	m := insertMounted(t, s, root)
	for _, file := range []string{"B/C/01 First.wav", "C/D/01 Second.wav"} {
		p := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		buildWAV(t, p)
	}
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "first", RelPath: "B/C/01 First.wav", Title: "First", Artist: "A", Decodable: true})
	seedOrganizationTrack(t, s, m.ID, root, Track{ID: "second", RelPath: "C/D/01 Second.wav", Title: "Second", Artist: "B", Decodable: true})
	for _, artist := range []string{"A", "B"} {
		album := ""
		if err := s.SaveFavorite(FavoriteRequest{Kind: "album", Artist: &artist, Album: &album}); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.FavoriteRisks) != 2 || preview.FavoriteRisks[0].Action != "migrate" || preview.FavoriteRisks[1].Action != "migrate" {
		t.Fatalf("favorite risks = %+v", preview.FavoriteRisks)
	}
	result, err := s.ApplyMetadataOrganization(t.Context(), preview.PreviewToken, false)
	if err != nil || result.FavoriteMigrated != 2 || result.FavoriteMerged != 0 {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	favorites, err := s.ListFavoriteAlbums()
	if err != nil || len(favorites) != 2 {
		t.Fatalf("favorites = %+v, %v", favorites, err)
	}
	got := map[metadataAlbumKey]bool{}
	for _, favorite := range favorites {
		got[metadataAlbumKey{artist: favorite.Artist, album: favorite.Album}] = true
	}
	if !got[metadataAlbumKey{artist: "B", album: "C"}] || !got[metadataAlbumKey{artist: "C", album: "D"}] {
		t.Fatalf("favorites = %+v, %v", favorites, err)
	}
}

func TestMetadataOrganizationFavoriteGraph(t *testing.T) {
	key := func(artist, album string) metadataAlbumKey { return metadataAlbumKey{artist: artist, album: album} }
	for _, tc := range []struct {
		name     string
		targets  map[metadataAlbumKey]map[metadataAlbumKey]struct{}
		favorite []metadataAlbumKey
		actions  map[int64]string
	}{
		{
			name: "chain resolves to final identity",
			targets: map[metadataAlbumKey]map[metadataAlbumKey]struct{}{
				key("A", ""): {key("B", ""): {}},
				key("B", ""): {key("C", "Y"): {}},
			},
			favorite: []metadataAlbumKey{key("A", ""), key("B", "")},
			actions:  map[int64]string{1: "migrate", 2: "merge"},
		},
		{
			name: "cycle stays stable",
			targets: map[metadataAlbumKey]map[metadataAlbumKey]struct{}{
				key("A", "X"): {key("B", "X"): {}},
				key("B", "X"): {key("A", "X"): {}},
			},
			favorite: []metadataAlbumKey{key("A", "X"), key("B", "X")},
			actions:  map[int64]string{1: "keep", 2: "keep"},
		},
		{
			name: "split keeps original favorite",
			targets: map[metadataAlbumKey]map[metadataAlbumKey]struct{}{
				key("A", ""): {key("B", "X"): {}, key("C", "Y"): {}},
			},
			favorite: []metadataAlbumKey{key("A", "")},
			actions:  map[int64]string{1: "keep"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			favorites := make([]metadataFavoriteIdentity, 0, len(tc.favorite))
			for i, k := range tc.favorite {
				favorites = append(favorites, metadataFavoriteIdentity{id: int64(i + 1), artist: k.artist, album: k.album})
			}
			risks, moves := metadataFavoriteRisks(tc.targets, map[metadataAlbumKey]int{}, favorites)
			actions := map[int64]string{}
			for _, risk := range risks {
				actions[risk.ID] = risk.Action
			}
			if len(actions) != len(tc.actions) {
				t.Fatalf("actions = %+v, risks = %+v", actions, risks)
			}
			for id, action := range tc.actions {
				if actions[id] != action {
					t.Fatalf("action %d = %q, want %q; risks = %+v", id, actions[id], action, risks)
				}
			}
			if len(tc.actions) == 0 && len(moves) != 0 {
				t.Fatalf("stable graph produced moves: %+v", moves)
			}
		})
	}
}

func TestMetadataOrganizationHashAndCueTagReads(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rel      string
		cueStart uint64
		track    int
		reads    int
	}{
		{name: "literal hash", rel: "Love #1.wav", reads: 1},
		{name: "literal cue-looking hash", rel: "Love #01", reads: 1},
		{name: "cue virtual track", rel: "Artist/Album/image.wav#01", cueStart: 1, track: 1, reads: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
			root := t.TempDir()
			m := insertMounted(t, s, root)
			p := filepath.Join(root, filepath.FromSlash(tc.rel))
			if _, cue := cueAudioRel(tc.rel); tc.cueStart > 0 && cue {
				p = filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(tc.rel, "#01")))
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			buildWAV(t, p)
			seedOrganizationTrack(t, s, m.ID, root, Track{ID: "track", RelPath: tc.rel, CueStart: tc.cueStart, TrackNumber: tc.track, Decodable: true})
			preview, err := s.PreviewMetadataOrganization(t.Context(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if preview.TagReads != tc.reads || preview.Count != 1 || preview.Unavailable != 0 {
				t.Fatalf("preview = %+v", preview)
			}
			if tc.name == "literal cue-looking hash" && (len(preview.Samples) != 1 || preview.Samples[0].NewTitle != "Love #01") {
				t.Fatalf("literal hash title changed: %+v", preview.Samples)
			}
		})
	}
}
