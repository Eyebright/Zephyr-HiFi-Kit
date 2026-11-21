package media

import "testing"

func TestParseCueTracksAndMetadata(t *testing.T) {
	text := "\uFEFFREM GENRE \"Jazz\"\nREM DATE 1999\nPERFORMER \"Album Artist\"\nTITLE \"Album Title\"\n" +
		"FILE \"album image.wav\" WAVE\n" +
		"  TRACK 01 AUDIO\n    TITLE \"First Live Song\"\n    INDEX 01 00:00:00\n" +
		"  TRACK 02 AUDIO\n    TITLE \"Second\"\n    PERFORMER \"Guest\"\n    INDEX 01 00:02:37\n"
	sheet, err := ParseCue(text)
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Album != "Album Title" || sheet.Performer != "Album Artist" || sheet.Genre != "Jazz" || sheet.Year != 1999 {
		t.Fatalf("sheet = %+v", sheet)
	}
	if len(sheet.Files) != 1 || sheet.Files[0].Name != "album image.wav" {
		t.Fatalf("files = %+v", sheet.Files)
	}
	tracks := sheet.Files[0].Tracks
	if len(tracks) != 2 || tracks[0].Number != 1 || tracks[1].Number != 2 {
		t.Fatalf("tracks = %+v", tracks)
	}
	if tracks[0].Title != "First Live Song" || tracks[0].StartFrames != 0 {
		t.Fatalf("track1 = %+v", tracks[0])
	}
	// 00:02:37 = 2*75 + 37 frames.
	if tracks[1].StartFrames != 187 || tracks[1].Performer != "Guest" {
		t.Fatalf("track2 = %+v", tracks[1])
	}
	if FramesToSamples(75, 44100) != 44100 || FramesToSamples(0, 44100) != 0 {
		t.Fatal("frame conversion")
	}
}

func TestParseCueRejectsEmptySheet(t *testing.T) {
	if _, err := ParseCue("TITLE \"no tracks\"\n"); err != ErrInvalidCue {
		t.Fatalf("err = %v", err)
	}
}
