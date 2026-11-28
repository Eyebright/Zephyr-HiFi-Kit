package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"hifihub/server/internal/media"
)

// LyricsLine is one displayable lyric line. TimeMS is zero for plain text.
type LyricsLine struct {
	TimeMS int64  `json:"time_ms"`
	Text   string `json:"text"`
}

// Lyrics is the server-cached lyric payload for one track.
type Lyrics struct {
	TrackID   string       `json:"track_id"`
	Source    string       `json:"source"`
	Synced    bool         `json:"synced"`
	Text      string       `json:"text,omitempty"`
	Lines     []LyricsLine `json:"lines"`
	UpdatedAt int64        `json:"updated_at,omitempty"`
}

const maxLyricsLines = 4096

var (
	lrcTimeRe     = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	lrcOffsetRe   = regexp.MustCompile(`(?i)\[offset:\s*([+-]?\d+)\s*\]`)
	lrcMetaRe     = regexp.MustCompile(`(?i)^\[(ti|ar|al|by|re|ve|length|au|lr|offset):[^\]]*\]\s*$`)
	lrcWordTimeRe = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
	// lrcAnyTimeRe strips every remaining timestamp token from the display text,
	// including inline square-bracket word tags ([00:18.70]) that enhanced LRC
	// uses between syllables, not just the angle-bracket form.
	lrcAnyTimeRe = regexp.MustCompile(`[\[<]\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?[\]>]`)
	spaceRunRe   = regexp.MustCompile(`\s+`)
)

// GetLyrics returns cached lyrics, extracting them lazily on first use so an
// existing library does not need a full rescan before the terminal can display
// lyrics. Embedded lyrics and same-name .lrc sidecars are both considered;
// synchronised text wins over plain text.
func (s *Service) GetLyrics(ctx context.Context, trackID string) (Lyrics, error) {
	t, err := s.GetTrack(trackID)
	if err != nil {
		return Lyrics{}, err
	}
	if cached, ok, err := s.loadCachedLyrics(t); err != nil {
		return Lyrics{}, err
	} else if ok {
		return cached, nil
	}
	path, err := s.PlaybackPath(trackID)
	if err != nil {
		return Lyrics{}, err
	}
	m, err := s.GetMount(t.MountID)
	if err != nil {
		return Lyrics{}, err
	}
	candidates := extractLyricsBounded(path, t.Container, m.TextEncoding)
	out := Lyrics{TrackID: trackID, Source: "none", Lines: []LyricsLine{}, UpdatedAt: time.Now().Unix()}
	var plain *lyricCandidate
	for i := range candidates {
		lines, synced := parseLyricsText(candidates[i].text)
		if len(lines) == 0 {
			continue
		}
		if synced {
			out.Source = candidates[i].source
			out.Synced = true
			out.Text = candidates[i].text
			out.Lines = lines
			break
		}
		if plain == nil {
			plain = &candidates[i]
			out.Source = plain.source
			out.Text = plain.text
			out.Lines = lines
		}
	}
	// Cache every outcome, including "none", so each track is read from the
	// source only once. A changed audio file (mtime/size) or a newly added
	// sidecar invalidates the cached result on the next request.
	if err := s.storeLyrics(ctx, t, path, out); err != nil {
		return Lyrics{}, err
	}
	return out, nil
}

type lyricCandidate struct{ source, text string }

// extractLyricsBounded reads embedded and sidecar lyrics off the source mount
// with a hard deadline so a stalled network share cannot hang the request (and
// pile up goroutines) indefinitely. A late read is simply discarded.
func extractLyricsBounded(path, container, encoding string) []lyricCandidate {
	done := make(chan []lyricCandidate, 1)
	go func() {
		var c []lyricCandidate
		if text, err := media.ReadEmbeddedLyrics(path, container, encoding); err == nil && strings.TrimSpace(text) != "" {
			c = append(c, lyricCandidate{"embedded", text})
		}
		if text, err := media.ReadSidecarLyrics(path, encoding); err == nil && strings.TrimSpace(text) != "" {
			c = append(c, lyricCandidate{"sidecar", text})
		}
		done <- c
	}()
	select {
	case c := <-done:
		return c
	case <-time.After(6 * time.Second):
		return nil
	}
}

func (s *Service) loadCachedLyrics(t Track) (Lyrics, bool, error) {
	var source, text, linesJSON string
	var synced int
	var mtime, size, updated int64
	err := s.db.QueryRow(`SELECT source,synced,text,lines,source_mtime_ns,source_size,updated_at
		FROM track_lyrics WHERE track_id=?`, t.ID).
		Scan(&source, &synced, &text, &linesJSON, &mtime, &size, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Lyrics{}, false, nil
	}
	if err != nil {
		return Lyrics{}, false, err
	}
	switch source {
	case "embedded", "none":
		if t.MtimeNS == 0 || t.Size == 0 || mtime != t.MtimeNS || size != t.Size {
			return Lyrics{}, false, nil
		}
		// A cached "no lyrics" result must still yield to a sidecar that was
		// added after the audio file was last read.
		if source == "none" {
			if _, ok := media.SidecarLyricsPath(t.Path); ok {
				return Lyrics{}, false, nil
			}
		}
	case "sidecar":
		p, ok := media.SidecarLyricsPath(t.Path)
		if !ok {
			return Lyrics{}, false, nil
		}
		st, err := os.Stat(p)
		if err != nil || st.Size() != size || st.ModTime().UnixNano() != mtime {
			return Lyrics{}, false, nil
		}
	default:
		return Lyrics{}, false, nil
	}
	var lines []LyricsLine
	if err := json.Unmarshal([]byte(linesJSON), &lines); err != nil {
		return Lyrics{}, false, nil
	}
	if lines == nil {
		lines = []LyricsLine{}
	}
	return Lyrics{TrackID: t.ID, Source: source, Synced: synced == 1, Text: text, Lines: lines, UpdatedAt: updated}, true, nil
}

func (s *Service) storeLyrics(ctx context.Context, t Track, path string, l Lyrics) error {
	mtime, size := t.MtimeNS, t.Size
	if l.Source == "sidecar" {
		if p, ok := media.SidecarLyricsPath(path); ok {
			if st, err := os.Stat(p); err == nil {
				mtime, size = st.ModTime().UnixNano(), st.Size()
			}
		}
	}
	lines, err := json.Marshal(l.Lines)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO track_lyrics
		(track_id,source,synced,text,lines,source_mtime_ns,source_size,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(track_id) DO UPDATE SET source=excluded.source,synced=excluded.synced,
		text=excluded.text,lines=excluded.lines,source_mtime_ns=excluded.source_mtime_ns,
		source_size=excluded.source_size,updated_at=excluded.updated_at`,
		t.ID, l.Source, boolInt(l.Synced), l.Text, string(lines), mtime, size, l.UpdatedAt)
	return err
}

func parseLyricsText(raw string) ([]LyricsLine, bool) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	offset := 0
	if m := lrcOffsetRe.FindStringSubmatch(raw); m != nil {
		offset, _ = strconv.Atoi(m[1])
	}
	var timed []LyricsLine
	var plain []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || lrcMetaRe.MatchString(line) {
			continue
		}
		var times []int64
		rest := line
		for {
			m := lrcTimeRe.FindStringSubmatch(rest)
			if m == nil {
				break
			}
			minutes, _ := strconv.Atoi(m[1])
			seconds, _ := strconv.Atoi(m[2])
			frac := 0
			if m[3] != "" {
				frac, _ = strconv.Atoi(m[3])
				switch len(m[3]) {
				case 1:
					frac *= 100
				case 2:
					frac *= 10
				}
			}
			rest = rest[len(m[0]):]
			if seconds > 59 {
				continue
			}
			ms := int64(minutes)*60000 + int64(seconds)*1000 + int64(frac) + int64(offset)
			if ms < 0 {
				ms = 0
			}
			times = append(times, ms)
		}
		text := lrcWordTimeRe.ReplaceAllString(rest, "")
		text = lrcAnyTimeRe.ReplaceAllString(text, "")
		text = strings.TrimSpace(spaceRunRe.ReplaceAllString(text, " "))
		if len(times) > 0 {
			for _, ms := range times {
				timed = append(timed, LyricsLine{TimeMS: ms, Text: text})
			}
		} else if text != "" && !strings.HasPrefix(text, "[") {
			plain = append(plain, text)
		}
	}
	if len(timed) > 0 {
		sort.SliceStable(timed, func(i, j int) bool { return timed[i].TimeMS < timed[j].TimeMS })
		if len(timed) > maxLyricsLines {
			timed = timed[:maxLyricsLines]
		}
		return timed, true
	}
	lines := make([]LyricsLine, 0, len(plain))
	for _, text := range plain {
		lines = append(lines, LyricsLine{Text: text})
	}
	if len(lines) > maxLyricsLines {
		lines = lines[:maxLyricsLines]
	}
	return lines, false
}
