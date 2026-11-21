package library

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCueWAV(t *testing.T, path string, frames int) {
	t.Helper()
	pcm := make([]byte, frames*4)
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], 2)
	binary.LittleEndian.PutUint32(hdr[24:28], 44100)
	binary.LittleEndian.PutUint32(hdr[28:32], 44100*4)
	binary.LittleEndian.PutUint16(hdr[32:34], 4)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	if err := os.WriteFile(path, append(hdr[:], pcm...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanExpandsCueSheetIntoVirtualTracks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCueWAV(t, filepath.Join(dir, "image.wav"), 3*44100)
	cue := `PERFORMER "Album Artist"
TITLE "Cue Album"
REM GENRE "Jazz"
REM DATE 1999
FILE "image.wav" WAVE
  TRACK 01 AUDIO
    TITLE "First"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Second"
    PERFORMER "Guest"
    INDEX 01 00:01:00
  TRACK 03 AUDIO
    TITLE "Third"
    INDEX 01 00:02:00
`
	if err := os.WriteFile(filepath.Join(dir, "album.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := addMountedLocalSource(t, s, root)
	res, err := s.ScanChanges(context.Background(), m.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 || res.Removed != 0 {
		t.Fatalf("first scan = %+v", res)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks = %d: %+v", len(tracks), tracks)
	}
	byNum := map[int]Track{}
	for _, tr := range tracks {
		byNum[tr.TrackNumber] = tr
	}
	first, second, third := byNum[1], byNum[2], byNum[3]
	if first.Title != "First" || first.Album != "Cue Album" || first.AlbumArtist != "Album Artist" || first.Genre != "Jazz" || first.Year != 1999 {
		t.Fatalf("first = %+v", first)
	}
	if first.CueStart != 0 || first.CueEnd != 44100 || first.TotalSamples != 44100 || first.DurationMS != 1000 {
		t.Fatalf("first window = %+v", first)
	}
	if second.CueStart != 44100 || second.CueEnd != 88200 || second.Artist != "Guest" {
		t.Fatalf("second = %+v", second)
	}
	if third.CueStart != 88200 || third.CueEnd != 132300 || third.TotalSamples != 44100 {
		t.Fatalf("third = %+v", third)
	}
	if first.ID == "" || !strings.HasPrefix(first.ID, "trk-cue-") {
		t.Fatalf("id = %q", first.ID)
	}

	res, err = s.ScanChanges(context.Background(), m.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Updated != 0 || res.Removed != 0 {
		t.Fatalf("incremental scan = %+v", res)
	}
	if tracks, _ := s.ListTracks(m.ID); len(tracks) != 3 {
		t.Fatalf("incremental tracks = %d", len(tracks))
	}
}

func TestScanKeepsImageWhenCueAudioMissing(t *testing.T) {
	root := t.TempDir()
	writeCueWAV(t, filepath.Join(root, "image.wav"), 44100)
	cue := "FILE \"missing.wav\" WAVE\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n"
	if err := os.WriteFile(filepath.Join(root, "album.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	m := addMountedLocalSource(t, s, root)
	if _, err := s.ScanChanges(context.Background(), m.ID, true); err != nil {
		t.Fatal(err)
	}
	tracks, _ := s.ListTracks(m.ID)
	if len(tracks) != 1 || tracks[0].RelPath != "image.wav" {
		t.Fatalf("tracks = %+v", tracks)
	}
}
