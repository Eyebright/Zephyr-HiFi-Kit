package library

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const unidentifiedAlbumPrefix = "未识别｜"

var (
	metadataAnnotation = regexp.MustCompile(`\[[^\]]+\]|\([^\)]+\)`)
	metadataCatalog    = regexp.MustCompile(`^[A-Za-z]{1,4}-[0-9]{1,5}-\s*`)
)

var genericMetadataDirs = map[string]bool{
	"music": true, "audio": true, "albums": true, "album": true,
	"音乐": true, "音频": true, "专辑": true, "艺术家": true,
	"hi-res": true, "hires": true, "hi res": true, "无损": true,
	"download": true, "downloads": true,
}

var technicalMetadataTerms = []string{
	"dsd", "sacd", "hi-res", "hires", "hi res", "flac", "wav", "mp3", "m4a",
	"bit", "khz", "mhz", "blu-ray", "bluray",
}

var collectionMetadataTerms = []string{
	"单曲", "singles", "排行榜", "热歌", "歌单", "合集", "全集", "discography",
}

type metadataDirHint struct {
	files    int
	numbered int
	artists  map[string]struct{}
}

type metadataPathResolver struct {
	dirs map[string]metadataDirHint
}

type metadataFill struct {
	Title        bool
	Artist       bool
	Album        bool
	AlbumArtist  bool
	DiscNumber   bool
	TrackNumber  bool
	Unidentified bool
}

func newMetadataPathResolver(tracks []Track) *metadataPathResolver {
	r := &metadataPathResolver{dirs: map[string]metadataDirHint{}}
	seen := make(map[string]struct{}, len(tracks))
	for _, tr := range tracks {
		rel := normalizeRelPath(tr.RelPath)
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		dir := path.Dir(rel)
		h := r.dirs[dir]
		if h.artists == nil {
			h.artists = map[string]struct{}{}
		}
		h.files++
		_, fileArtist, _, trackNumber := metadataTrackFileParts(rel, tr.CueStart)
		if tr.TrackNumber > 0 || trackNumber > 0 {
			h.numbered++
		}
		if fileArtist != "" {
			h.artists[fileArtist] = struct{}{}
		} else if tr.Artist != "" {
			h.artists[tr.Artist] = struct{}{}
		}
		r.dirs[dir] = h
	}
	return r
}

func metadataScanResolver(previous, changed []Track, remaining map[string]Track) *metadataPathResolver {
	live := make(map[string]Track, len(previous)+len(changed))
	for _, tr := range previous {
		if _, deleted := remaining[tr.RelPath]; deleted {
			continue
		}
		live[normalizeRelPath(tr.RelPath)] = tr
	}
	for _, tr := range changed {
		live[normalizeRelPath(tr.RelPath)] = tr
	}
	tracks := make([]Track, 0, len(live))
	for _, tr := range live {
		tracks = append(tracks, tr)
	}
	return newMetadataPathResolver(tracks)
}

func normalizeRelPath(rel string) string {
	rel = strings.ReplaceAll(rel, `\`, "/")
	rel = path.Clean(rel)
	if rel == "." || rel == "/" {
		return ""
	}
	return strings.TrimPrefix(rel, "/")
}

func cueAudioRel(rel string) (string, bool) {
	rel = normalizeRelPath(rel)
	i := strings.LastIndexByte(rel, '#')
	if i < 0 || len(rel)-i != 3 {
		return "", false
	}
	n, err := strconv.Atoi(rel[i+1:])
	return rel[:i], err == nil && n > 0 && n <= 99
}

func metadataTrackFileParts(rel string, cueStart uint64) (stem, artist, title string, trackNumber int) {
	rel = normalizeRelPath(rel)
	if cueStart > 0 {
		if audioRel, ok := cueAudioRel(rel); ok {
			rel = audioRel
		}
	}
	return metadataFileParts(rel)
}

func metadataFileParts(rel string) (stem, artist, title string, trackNumber int) {
	base := path.Base(normalizeRelPath(rel))
	stem = strings.TrimSuffix(base, path.Ext(base))
	trackNumber = leadingTrackNumber(stem)
	stem = strings.TrimSpace(stripTrackPrefix(stem))
	if i := strings.Index(stem, " - "); i > 0 {
		artist = strings.TrimSpace(stem[:i])
		title = strings.TrimSpace(stem[i+3:])
		if title == "" {
			artist = ""
		}
	}
	if title == "" {
		title = stem
	}
	return stem, artist, title, trackNumber
}

func leadingTrackNumber(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i > 3 || i == len(s) {
		return 0
	}
	switch s[i] {
	case ' ', '.', '-', '_':
	default:
		return 0
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil || n <= 0 || n > 999 {
		return 0
	}
	return n
}

func metadataLabel(s string) string {
	s = metadataAnnotation.ReplaceAllStringFunc(s, func(annotation string) string {
		if containsMetadataTerm(annotation, technicalMetadataTerms) {
			return " "
		}
		return annotation
	})
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " .-_")
}

func containsMetadataTerm(s string, terms []string) bool {
	lower := strings.ToLower(s)
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func genericMetadataDir(s string) bool {
	return genericMetadataDirs[strings.ToLower(strings.TrimSpace(s))]
}

func technicalOnlyMetadataDir(s string) bool {
	label := strings.ToLower(metadataLabel(s))
	switch label {
	case "dsd", "sacd", "flac", "wav", "mp3", "m4a", "hi-res", "hires", "hi res":
		return true
	}
	if genericMetadataDirs[label] && containsMetadataTerm(label, technicalMetadataTerms) {
		return true
	}
	for _, prefix := range []string{"dsd", "sacd"} {
		if strings.HasPrefix(label, prefix) {
			rest := strings.TrimPrefix(label, prefix)
			if rest == "" {
				return true
			}
			numeric := true
			for _, r := range rest {
				if !unicode.IsDigit(r) {
					numeric = false
					break
				}
			}
			if numeric {
				return true
			}
		}
	}
	return false
}

func collectionMetadataDir(s string) bool {
	return containsMetadataTerm(s, collectionMetadataTerms)
}

func discNumberFromDir(s string) int {
	s = strings.ToLower(strings.TrimSpace(metadataLabel(s)))
	for _, prefix := range []string{"disc", "disk", "cd"} {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(s, prefix))
		n, err := strconv.Atoi(rest)
		if err == nil && n > 0 && n <= 99 {
			return n
		}
	}
	return 0
}

func splitMetadataArtistAlbum(s string) (string, string) {
	s = strings.TrimSpace(metadataCatalog.ReplaceAllString(s, ""))
	if i := strings.Index(s, " - "); i > 0 {
		artist := strings.TrimSpace(s[:i])
		album := strings.TrimSpace(s[i+3:])
		if artist != "" && album != "" {
			return artist, album
		}
	}
	return "", ""
}

func artistFromCollectionDir(s string) string {
	if !collectionMetadataDir(s) {
		return ""
	}
	for _, sep := range []string{" - ", "-", "_"} {
		if i := strings.Index(s, sep); i > 0 {
			artist := strings.TrimSpace(s[:i])
			if artist != "" && !genericMetadataDir(artist) && !looksNumeric(artist) {
				return artist
			}
		}
	}
	return ""
}

func looksNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

func (r *metadataPathResolver) apply(tr *Track) metadataFill {
	var fill metadataFill
	rel := normalizeRelPath(tr.RelPath)
	if rel == "" {
		return fill
	}
	_, fileArtist, fileTitle, fileTrack := metadataTrackFileParts(rel, tr.CueStart)
	if tr.Title == "" {
		tr.Title = fileTitle
		fill.Title = tr.Title != ""
	}
	if tr.TrackNumber == 0 && fileTrack > 0 {
		tr.TrackNumber = fileTrack
		fill.TrackNumber = true
	}

	dirs := []string{}
	if dir := path.Dir(rel); dir != "." {
		dirs = strings.Split(dir, "/")
	}
	discNumber := 0
	end := len(dirs)
	for end > 0 {
		if n := discNumberFromDir(dirs[end-1]); n > 0 {
			discNumber = n
			end--
			continue
		}
		label := metadataLabel(dirs[end-1])
		if label == "" || technicalOnlyMetadataDir(dirs[end-1]) {
			end--
			continue
		}
		break
	}
	if tr.DiscNumber == 0 && discNumber > 0 {
		tr.DiscNumber = discNumber
		fill.DiscNumber = true
	}

	pathArtist, albumPathArtist, pathAlbum := "", "", ""
	inferredAlbum := false
	albumWasMissing := tr.Album == ""
	flatAlbumDir := len(dirs) <= 1
	flatParentArtist := ""
	if end > 0 {
		albumDir := dirs[end-1]
		albumLabel := metadataLabel(albumDir)
		parentLabel := ""
		if end > 1 {
			parentLabel = metadataLabel(dirs[end-2])
		}
		parentUsable := parentLabel != "" && !genericMetadataDir(parentLabel) &&
			!containsMetadataTerm(dirs[end-2], technicalMetadataTerms)
		if parentUsable && parentLabel != albumLabel {
			flatParentArtist = parentLabel
		}
		flatSinglesDir := collectionMetadataDir(albumDir)
		if flatSinglesDir {
			flatAlbumDir = true
		}
		hint := r.dirs[path.Dir(rel)]
		strongAlbumDir := !flatSinglesDir && end > 1 && (parentUsable || parentLabel == albumLabel)
		if !flatSinglesDir && !strongAlbumDir {
			strongAlbumDir = hint.files >= 2 && hint.numbered*5 >= hint.files*4
		}
		if strongAlbumDir && albumLabel != "" {
			inferredAlbum = true
			pathAlbum = albumLabel
			if parentUsable && parentLabel != albumLabel {
				pathArtist = parentLabel
				albumPathArtist = pathArtist
			} else {
				pathArtist, pathAlbum = splitMetadataArtistAlbum(albumLabel)
				albumPathArtist = pathArtist
			}
		}
	}

	if !inferredAlbum && len(dirs) > 0 {
		pathArtist = artistFromCollectionDir(metadataLabel(dirs[len(dirs)-1]))
		if pathArtist == "" && flatAlbumDir {
			pathArtist = flatParentArtist
		}
	}
	if pathArtist == "" {
		pathArtist = fileArtist
	}
	if tr.Artist == "" && pathArtist != "" {
		tr.Artist = pathArtist
		fill.Artist = true
	}

	hint := r.dirs[path.Dir(rel)]
	if inferredAlbum && tr.Album == "" {
		tr.Album = pathAlbum
		if tr.Album == "" {
			tr.Album = metadataLabel(dirs[end-1])
		}
		fill.Album = tr.Album != ""
	}
	if inferredAlbum && tr.Artist == "" && len(hint.artists) == 1 {
		for artist := range hint.artists {
			tr.Artist = artist
			fill.Artist = true
		}
	}
	if inferredAlbum && albumWasMissing && tr.AlbumArtist == "" {
		switch {
		case albumPathArtist != "":
			tr.AlbumArtist = albumPathArtist
		case len(hint.artists) == 1:
			for artist := range hint.artists {
				tr.AlbumArtist = artist
			}
		case len(hint.artists) > 1:
			tr.AlbumArtist = "群星"
		}
		fill.AlbumArtist = tr.AlbumArtist != ""
	}
	if tr.Album == "" {
		if flatAlbumDir {
			tr.Album = unidentifiedAlbumPrefix + fileTitle
		} else {
			tr.Album = unidentifiedAlbumPrefix + path.Dir(rel)
		}
		fill.Album = true
		fill.Unidentified = true
	}
	return fill
}
