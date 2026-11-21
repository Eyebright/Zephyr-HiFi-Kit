package media

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// CueSheet is the subset of CUE metadata the library indexes.
type CueSheet struct {
	Album     string
	Performer string
	Genre     string
	Year      int
	Files     []CueFile
}

// CueFile maps one FILE block to its audio image and tracks.
type CueFile struct {
	Name   string
	Tracks []CueTrack
}

// CueTrack is one TRACK with its INDEX 01 position in CD frames (75 per second).
type CueTrack struct {
	Number      int
	Title       string
	Performer   string
	StartFrames int
}

// ErrInvalidCue reports a cue sheet without a usable FILE/TRACK/INDEX.
var ErrInvalidCue = errors.New("cue: no usable file/track/index")

const maxCueBytes = 1 << 20

// ReadCueSheet parses a cue sheet; encoding follows the source setting and only
// applies when the bytes are not valid UTF-8 (same rules as tag text).
func ReadCueSheet(path, encoding string) (CueSheet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CueSheet{}, err
	}
	if len(data) > maxCueBytes {
		data = data[:maxCueBytes]
	}
	text := decodeTagText(data, encoding)
	if strings.TrimSpace(text) == "" && len(data) > 0 {
		return CueSheet{}, ErrInvalidCue
	}
	return ParseCue(text)
}

// ParseCue parses decoded cue text.
func ParseCue(text string) (CueSheet, error) {
	sheet := CueSheet{}
	var file *CueFile
	var track *CueTrack
	flush := func() {
		if file != nil && len(file.Tracks) > 0 {
			sheet.Files = append(sheet.Files, *file)
		}
		file, track = nil, nil
	}
	for _, raw := range strings.Split(strings.TrimPrefix(text, "\uFEFF"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		key, rest := splitCueCommand(line)
		switch strings.ToUpper(key) {
		case "FILE":
			flush()
			name := cueQuoted(rest)
			if name == "" {
				continue
			}
			file = &CueFile{Name: name}
		case "TRACK":
			if file == nil {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			n, err := strconv.Atoi(fields[0])
			if err != nil || n <= 0 {
				continue
			}
			file.Tracks = append(file.Tracks, CueTrack{Number: n, StartFrames: -1})
			track = &file.Tracks[len(file.Tracks)-1]
		case "INDEX":
			if track == nil {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) < 2 || fields[0] != "01" {
				continue
			}
			frames, ok := cueTime(fields[1])
			if ok && track.StartFrames < 0 {
				track.StartFrames = frames
			}
		case "TITLE":
			if track != nil {
				track.Title = cueQuoted(rest)
			} else {
				sheet.Album = cueQuoted(rest)
			}
		case "PERFORMER":
			if track != nil {
				track.Performer = cueQuoted(rest)
			} else {
				sheet.Performer = cueQuoted(rest)
			}
		case "REM":
			if len(rest) > 4 && strings.EqualFold(rest[:4], "DATE") {
				if y, ok := cueYear(strings.TrimSpace(rest[4:])); ok {
					sheet.Year = y
				}
			}
			if len(rest) > 5 && strings.EqualFold(rest[:5], "GENRE") {
				sheet.Genre = cueQuoted(strings.TrimSpace(rest[5:]))
			}
		}
	}
	flush()
	valid := false
	for _, f := range sheet.Files {
		kept := f.Tracks[:0]
		for _, t := range f.Tracks {
			if t.StartFrames >= 0 {
				kept = append(kept, t)
				valid = true
			}
		}
		f.Tracks = kept
	}
	if !valid {
		return CueSheet{}, ErrInvalidCue
	}
	return sheet, nil
}

func splitCueCommand(line string) (string, string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// cueQuoted returns the first quoted token, or the whole text when unquoted.
func cueQuoted(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if j := strings.Index(s[1:], "\""); j >= 0 {
			return s[1 : 1+j]
		}
		return s[1:]
	}
	if i := strings.LastIndexAny(s, " \t"); i > 0 {
		// FILE name WAVE: the type follows the name.
		tail := strings.ToUpper(strings.TrimSpace(s[i+1:]))
		if tail == "WAVE" || tail == "MP3" || tail == "BINARY" || tail == "AIFF" || tail == "MOTOROLA" {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// cueTime converts mm:ss:ff into CD frames (75 frames per second).
func cueTime(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	mm, err1 := strconv.Atoi(parts[0])
	ss, err2 := strconv.Atoi(parts[1])
	ff, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || mm < 0 || ss < 0 || ss >= 60 || ff < 0 || ff >= 75 {
		return 0, false
	}
	return mm*60*75 + ss*75 + ff, true
}

func cueYear(s string) (int, bool) {
	s = cueQuoted(s)
	if len(s) < 4 {
		return 0, false
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y < 1 || y > 9999 {
		return 0, false
	}
	return y, true
}

// FramesToSamples converts CD frames at 75 per second into PCM samples.
func FramesToSamples(frames, rate int) uint64 {
	if frames <= 0 || rate <= 0 {
		return 0
	}
	return uint64(frames) * uint64(rate) / 75
}
