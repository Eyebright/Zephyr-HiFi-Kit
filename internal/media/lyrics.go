package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrNoLyrics reports that neither the container nor a sidecar carries lyrics.
var ErrNoLyrics = errors.New("no lyrics")

const maxLyricsBytes = 256 << 10

// ReadEmbeddedLyrics extracts unsynchronised or synchronised lyrics from the
// container's native tag area. Containers without a lyrics convention report
// ErrNoLyrics so callers can fall back to a same-name .lrc sidecar.
func ReadEmbeddedLyrics(path, container, encoding string) (string, error) {
	switch container {
	case "flac", "mp3", "dsf", "dff", "wav", "aiff", "adts":
		tags, err := ReadTagsWithEncoding(path, encoding)
		if strings.TrimSpace(tags.Lyrics) != "" {
			return tags.Lyrics, nil
		}
		if err != nil {
			return "", err
		}
		return "", ErrNoLyrics
	}
	return "", ErrNoLyrics
}

// SidecarLyricsPath finds a same-name .lrc file next to the audio file.
func SidecarLyricsPath(audioPath string) (string, bool) {
	dir := filepath.Dir(audioPath)
	base := filepath.Base(audioPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(e.Name(), stem+".lrc") {
			continue
		}
		return filepath.Join(dir, e.Name()), true
	}
	return "", false
}

// ReadSidecarLyrics reads a same-name .lrc file using the source text encoding.
func ReadSidecarLyrics(audioPath, encoding string) (string, error) {
	p, ok := SidecarLyricsPath(audioPath)
	if !ok {
		return "", ErrNoLyrics
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > maxLyricsBytes {
		return "", fmt.Errorf("lyrics file is not regular or too large: %s", p)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLyricsBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxLyricsBytes {
		return "", fmt.Errorf("lyrics file too large: %s", p)
	}
	text := strings.TrimPrefix(decodeTagText(data, encoding), "\ufeff")
	if strings.TrimSpace(text) == "" {
		return "", ErrNoLyrics
	}
	return text, nil
}

func (tags *Tags) setLyrics(value string) {
	value = strings.TrimSpace(strings.Trim(value, "\x00"))
	if value == "" || len(value) > maxLyricsBytes || !utf8.ValidString(value) {
		return
	}
	if tags.Lyrics == "" {
		tags.Lyrics = value
	}
}

func isLyricsKey(key string) bool {
	switch key {
	case "LYRICS", "UNSYNCEDLYRICS", "SYNCEDLYRICS", "LRC", "UNSYNCED LYRICS":
		return true
	}
	return false
}

func parseID3LyricsFrame(id string, data []byte, encoding string) (string, bool) {
	switch id {
	case "ULT", "USLT":
		return parseUSLT(data, encoding)
	case "SLT", "SYLT":
		return parseSYLT(data, encoding)
	}
	return "", false
}

func parseUSLT(data []byte, encoding string) (string, bool) {
	if len(data) < 5 {
		return "", false
	}
	coding := data[0]
	text, ok := splitID3LyricsDescriptor(coding, data[4:])
	if !ok {
		return "", false
	}
	s := decodeID3LyricsText(coding, text, encoding)
	return s, s != ""
}

// parseSYLT converts millisecond-timestamped ID3 synchronised lyrics into LRC
// lines. MPEG-frame timestamps need the audio frame rate and are not guessed.
func parseSYLT(data []byte, encoding string) (string, bool) {
	if len(data) < 7 || data[4] != 2 || data[5] != 1 {
		return "", false
	}
	coding := data[0]
	text, ok := splitID3LyricsDescriptor(coding, data[6:])
	if !ok {
		return "", false
	}
	var out strings.Builder
	for len(text) > 0 {
		raw, rest, ok := cutID3LyricsString(coding, text)
		if !ok || len(rest) < 4 {
			return "", false
		}
		ms := binary.BigEndian.Uint32(rest[:4])
		text = rest[4:]
		s := decodeID3LyricsText(coding, raw, encoding)
		if s == "" {
			continue
		}
		fmt.Fprintf(&out, "[%02d:%02d.%03d]%s\n", ms/60000, (ms/1000)%60, ms%1000, s)
	}
	return out.String(), out.Len() > 0
}

func splitID3LyricsDescriptor(coding byte, b []byte) ([]byte, bool) {
	_, rest, ok := cutID3LyricsString(coding, b)
	return rest, ok
}

func cutID3LyricsString(coding byte, b []byte) ([]byte, []byte, bool) {
	if coding == 1 || coding == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return b[:i], b[i+2:], true
			}
		}
		return nil, nil, false
	}
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return nil, nil, false
	}
	return b[:i], b[i+1:], true
}

func decodeID3LyricsText(coding byte, b []byte, encoding string) string {
	values := id3TextValues(append([]byte{coding}, b...), 3, encoding)
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}
