package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"hifihub/server/internal/media"
)

// cueJob is one parsed .cue file discovered during the scan walk.
type cueJob struct {
	rel     string
	abs     string
	mtimeNS int64
	sheet   media.CueSheet
}

func newCueJob(root, abs string, info fs.FileInfo, encoding string) (cueJob, bool) {
	sheet, err := media.ReadCueSheet(abs, encoding)
	if err != nil {
		return cueJob{}, false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return cueJob{}, false
	}
	return cueJob{rel: filepath.ToSlash(rel), abs: abs, mtimeNS: info.ModTime().UnixNano(), sheet: sheet}, true
}

// resolveCueAudio maps a FILE name to the on-disk image (case-insensitive on
// SMB shares that ignore case).
func resolveCueAudio(root, cueAbs, name string) (rel, abs string, ok bool) {
	if name == "" {
		return "", "", false
	}
	dir := filepath.Dir(cueAbs)
	cand := filepath.Join(dir, filepath.FromSlash(name))
	if withinRoot(root, cand) {
		if fi, err := os.Stat(cand); err == nil && fi.Mode().IsRegular() {
			if rel, err := filepath.Rel(root, cand); err == nil {
				return filepath.ToSlash(rel), cand, true
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", false
	}
	want := strings.ToLower(filepath.Base(name))
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(e.Name()) != want {
			continue
		}
		cand = filepath.Join(dir, e.Name())
		if rel, err := filepath.Rel(root, cand); err == nil {
			return filepath.ToSlash(rel), cand, true
		}
	}
	return "", "", false
}

func cueSplitID(mountID, audioRel string, number int) string {
	sum := sha256.Sum256([]byte(mountID + "|cue|" + audioRel + "|" + strconv.Itoa(number)))
	return "trk-cue-" + hex.EncodeToString(sum[:12])
}

func lookupSeen(seen map[string]fs.FileInfo, abs string) (fs.FileInfo, bool) {
	if info, ok := seen[abs]; ok {
		return info, true
	}
	want := strings.ToLower(filepath.Clean(abs))
	for p, info := range seen {
		if strings.ToLower(filepath.Clean(p)) == want {
			return info, true
		}
	}
	return nil, false
}

type cueExpandStats struct {
	added, updated, unchanged    int
	droppedAdded, droppedUpdated int
}

// expandCueJobs replaces image tracks referenced by cue sheets with virtual
// sample-window tracks. Only WAV (PCM) and FLAC images are split; other codecs
// keep the whole-file track because no bit-perfect window decoder exists.
func expandCueJobs(ctx context.Context, root, mountID string, jobs []cueJob, changed []Track, remaining map[string]Track, prev map[string]Track, seen map[string]fs.FileInfo, now int64) ([]Track, cueExpandStats, error) {
	stats := cueExpandStats{}
	if len(jobs) == 0 {
		return changed, stats, nil
	}
	idxChanged := make(map[string]int, len(changed))
	for i := range changed {
		idxChanged[changed[i].RelPath] = i
	}
	drop := make(map[int]bool)
	processed := make(map[string]bool)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].rel < jobs[j].rel })
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return changed, stats, err
		}
		sheet := job.sheet
		for _, file := range sheet.Files {
			audioRel, audioAbs, ok := resolveCueAudio(root, job.abs, file.Name)
			if !ok || processed[audioRel] {
				continue
			}
			var src Track
			srcOK := false
			if i, ok := idxChanged[audioRel]; ok && !drop[i] {
				src, srcOK = changed[i], true
			} else if t, ok := prev[audioRel]; ok {
				src, srcOK = t, true
			}
			if !srcOK || src.Rate <= 0 || src.TotalSamples == 0 {
				continue
			}
			if src.Codec != "flac" && !strings.HasPrefix(src.Codec, "pcm_") {
				continue
			}
			info, ok := lookupSeen(seen, audioAbs)
			if !ok {
				continue
			}
			tracks := append([]media.CueTrack{}, file.Tracks...)
			sort.SliceStable(tracks, func(i, j int) bool { return tracks[i].StartFrames < tracks[j].StartFrames })
			wantMtime := job.mtimeNS
			if mt := info.ModTime().UnixNano(); mt > wantMtime {
				wantMtime = mt
			}
			album := sheet.Album
			if album == "" {
				album = src.Album
			}
			if album == "" {
				album = pathBase(audioRel)
			}
			artist := sheet.Performer
			if artist == "" {
				artist = src.Artist
			}
			produced := 0
			for i, tr := range tracks {
				start := media.FramesToSamples(tr.StartFrames, src.Rate)
				if start >= src.TotalSamples {
					continue
				}
				end := src.TotalSamples
				for j := i + 1; j < len(tracks); j++ {
					if e := media.FramesToSamples(tracks[j].StartFrames, src.Rate); e > start {
						end = e
						break
					}
				}
				if end > src.TotalSamples {
					end = src.TotalSamples
				}
				if end <= start {
					continue
				}
				relV := audioRel + "#" + fmt.Sprintf("%02d", tr.Number)
				title := tr.Title
				if title == "" {
					title = fmt.Sprintf("Track %02d", tr.Number)
				}
				trArtist := tr.Performer
				if trArtist == "" {
					trArtist = artist
				}
				t := Track{
					ID: cueSplitID(mountID, audioRel, tr.Number), MountID: mountID,
					Path: audioAbs, RelPath: relV, AddedAt: now,
					Title: title, Artist: trArtist, Album: album, AlbumArtist: artist,
					Genre: sheet.Genre, Year: sheet.Year, DiscNumber: 1, TrackNumber: tr.Number,
					Codec: src.Codec, Container: src.Container, Rate: src.Rate, Channels: src.Channels, Bits: src.Bits,
					TotalSamples: end - start, CueStart: start, CueEnd: end,
					DurationMS: int((end - start) * 1000 / uint64(src.Rate)),
					Decodable:  true, MQAChecked: true, Size: info.Size(), MtimeNS: wantMtime, ScannedAt: now,
				}
				if old, exists := prev[relV]; exists {
					t.AddedAt = old.AddedAt
					if old.MtimeNS == wantMtime && old.Size == t.Size && old.CueStart == t.CueStart && old.CueEnd == t.CueEnd && old.TotalSamples == t.TotalSamples {
						stats.unchanged++
						delete(remaining, relV)
						produced++
						continue
					}
					stats.updated++
				} else {
					stats.added++
				}
				changed = append(changed, t)
				idxChanged[relV] = len(changed) - 1
				delete(remaining, relV)
				produced++
			}
			if produced == 0 {
				continue
			}
			processed[audioRel] = true
			// The whole-image track must not stay visible next to its cue tracks.
			if i, ok := idxChanged[audioRel]; ok && !drop[i] {
				drop[i] = true
				if _, live := prev[audioRel]; live {
					stats.droppedUpdated++
				} else {
					stats.droppedAdded++
				}
			}
			if old, ok := prev[audioRel]; ok {
				remaining[audioRel] = old
			}
		}
	}
	if len(drop) == 0 {
		return changed, stats, nil
	}
	kept := changed[:0]
	for i := range changed {
		if drop[i] {
			continue
		}
		kept = append(kept, changed[i])
	}
	return kept, stats, nil
}

func pathBase(rel string) string {
	dir := path.Dir(rel)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return path.Base(dir)
}
