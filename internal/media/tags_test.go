package media

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func vorbisComments(entries ...string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, 4)
	b = append(b, "test"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(entries)))
	for _, entry := range entries {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(entry)))
		b = append(b, entry...)
	}
	return b
}

func wavChunk(id string, data []byte) []byte {
	b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	b = append(b, data...)
	if len(data)%2 != 0 {
		b = append(b, 0)
	}
	return b
}

func taggedWAV(chunks ...[]byte) []byte {
	var format [16]byte
	binary.LittleEndian.PutUint16(format[:2], 1)
	binary.LittleEndian.PutUint16(format[2:4], 2)
	binary.LittleEndian.PutUint32(format[4:8], 44100)
	binary.LittleEndian.PutUint32(format[8:12], 44100*4)
	binary.LittleEndian.PutUint16(format[12:14], 4)
	binary.LittleEndian.PutUint16(format[14:], 16)
	b := []byte("RIFF\x00\x00\x00\x00WAVE")
	b = append(b, wavChunk("fmt ", format[:])...)
	// Metadata may follow a large audio chunk, beyond ProbeFile's header buffer.
	b = append(b, wavChunk("data", make([]byte, 128*1024))...)
	for _, chunk := range chunks {
		b = append(b, chunk...)
	}
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	return b
}

func TestReadTagsFLACBeyondLargePicture(t *testing.T) {
	p := writeFLAC(t,
		flacHead()[4:],
		flacBlock(6, false, pictureBody(3, "image/png", make([]byte, 256*1024))),
		flacBlock(4, true, vorbisComments("TITLE= 标签标题 ", "ARTIST=表演者", "ALBUM=专辑",
			"genre= \x00", "GeNrE= 爵士 ", "GENRE=Rock")),
	)
	before, err := ProbeFile(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadTags(p)
	want := Tags{Title: "标签标题", Artist: "表演者", Album: "专辑", Genre: "爵士; Rock"}
	if err != nil || got != want {
		t.Fatalf("tags = %+v, %v; want %+v", got, err, want)
	}
	after, err := ProbeFile(p)
	if err != nil || after != before || !after.Decodable || after.Rate != 44100 {
		t.Fatalf("tags changed FLAC probe: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestReadTagsWAVInfoAfterAudio(t *testing.T) {
	info := []byte("INFO")
	for _, field := range []struct{ id, value string }{
		{"INAM", " Title\x00"}, {"IART", "艺术家\x00"}, {"IPRD", " Album \x00"},
		{"IGNR", " \x00"}, {"IGNR", "\x00 Jazz \x00Rock"}, {"IGNR", "Classical\x00"},
	} {
		info = append(info, wavChunk(field.id, []byte(field.value))...)
	}
	data := taggedWAV(wavChunk("JUNK", []byte{1}), wavChunk("LIST", []byte("adtl")), wavChunk("LIST", info))
	p := filepath.Join(t.TempDir(), "tags.wave")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTags(p)
	want := Tags{Title: "Title", Artist: "艺术家", Album: "Album", Genre: "Jazz; Rock; Classical"}
	if err != nil || got != want {
		t.Fatalf("tags = %+v, %v; want %+v", got, err, want)
	}
	pr, err := ProbeFile(p)
	if err != nil || !pr.Decodable || pr.Rate != 44100 || pr.TotalSamples != 128*1024/4 {
		t.Fatalf("WAV probe = %+v, %v", pr, err)
	}
}

func TestReadTagsAbsentAndUnsupported(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty": nil,
		"short": []byte("abc"),
		"mp3":   []byte("ID3\x03\x00\x00\x00\x00\x00\x00"),
		"flac":  append([]byte("fLaC"), flacBlock(0, true, make([]byte, 34))...),
		"wav":   taggedWAV(wavChunk("LIST", []byte("INFO"))),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(data), int64(len(data)))
			if err != nil || got != (Tags{}) {
				t.Fatalf("tags = %+v, %v", got, err)
			}
		})
	}
	if _, err := ReadTags(filepath.Join(t.TempDir(), "missing.flac")); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestReadTagsMalformed(t *testing.T) {
	flac := func(comment []byte) []byte {
		return append(flacHead(), flacBlock(4, true, comment)...)
	}
	badCount := vorbisComments()
	binary.LittleEndian.PutUint32(badCount[8:12], ^uint32(0))
	badLength := vorbisComments("TITLE=Kept")
	binary.LittleEndian.PutUint32(badLength[8:12], 2)
	badLength = binary.LittleEndian.AppendUint32(badLength, ^uint32(0))
	badInfo := append([]byte("INFO"), wavChunk("INAM", []byte("Kept"))...)
	badInfo = append(badInfo, 'I', 'G', 'N', 'R', 0xff, 0xff, 0xff, 0xff)
	badRIFF := taggedWAV()
	binary.LittleEndian.PutUint32(badRIFF[4:8], ^uint32(0))
	for _, tc := range []struct {
		name string
		data []byte
		want Tags
	}{
		{"flac block length", append(flacHead(), 0x84, 0xff, 0xff, 0xff), Tags{}},
		{"flac vendor length", flac([]byte{0xff, 0xff, 0xff, 0xff}), Tags{}},
		{"flac count", flac(badCount), Tags{}},
		{"flac truncated value", flac(badLength), Tags{Title: "Kept"}},
		{"flac oversized block", flac(make([]byte, maxTagBytes+1)), Tags{}},
		{"wav chunk length", taggedWAV([]byte("LIST\xff\xff\xff\xffINFO")), Tags{}},
		{"wav info length", taggedWAV(wavChunk("LIST", badInfo)), Tags{Title: "Kept"}},
		{"wav trailing info", taggedWAV(wavChunk("LIST", []byte("INFOx"))), Tags{}},
		{"wav oversized list", taggedWAV(wavChunk("LIST", append([]byte("INFO"), make([]byte, maxTagBytes+1)...))), Tags{}},
		{"wav riff length", badRIFF, Tags{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(tc.data), int64(len(tc.data)))
			if err == nil || got != tc.want {
				t.Fatalf("tags = %+v, %v; want %+v and an error", got, err, tc.want)
			}
			if pr := Probe(tc.data, int64(len(tc.data))); !pr.Decodable {
				t.Fatalf("bad tags prevented basic identification: %+v", pr)
			}
		})
	}
}

func TestReadTagsValueLimits(t *testing.T) {
	long := strings.Repeat("a", maxTagValueBytes+1)
	comment := vorbisComments("TITLE="+long, "GENRE=\xff", "GENRE=", "GENRE= Soul ", "GENRE=Rock")
	got, err := parseVorbisTags(comment)
	if err != nil || got != (Tags{Genre: "Soul; Rock"}) {
		t.Fatalf("Vorbis value limits: %+v, %v", got, err)
	}
	info := append(wavChunk("INAM", []byte(long)), wavChunk("IGNR", []byte("Jazz"))...)
	var tags Tags
	if err := tags.parseWAVInfo(info); err != nil || tags != (Tags{Genre: "Jazz"}) {
		t.Fatalf("WAV value limits: %+v, %v", tags, err)
	}
}

func TestReadTagsBlockLimits(t *testing.T) {
	flac := flacHead()
	for i := 0; i < maxTagBlocks; i++ {
		flac = append(flac, flacBlock(1, false, nil)...)
	}
	wav := taggedWAV()
	for i := 0; i < maxTagBlocks; i++ {
		wav = append(wav, wavChunk("JUNK", nil)...)
	}
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	for _, data := range [][]byte{flac, wav} {
		if _, err := readTags(bytes.NewReader(data), int64(len(data))); err == nil {
			t.Fatal("expected metadata block limit")
		}
	}
}

func TestExtendedVorbisMetadata(t *testing.T) {
	got, err := parseVorbisTags(vorbisComments(
		"ALBUM ARTIST=王菲", "ALBUMARTIST=ignored", "COMPOSER=Bach, Johann Sebastian",
		"COMPOSER= Mozart ; Bach, Johann Sebastian", "COMPOSER=mozart", "GENRE=Jazz; Rock", "GENRE=jazz",
		"DATE=1998-05-09", "YEAR=2001", "DISCNUMBER=2/3", "TRACKNUMBER=07/12"))
	want := Tags{AlbumArtist: "王菲", Composer: "Bach, Johann Sebastian; Mozart", Genre: "Jazz; Rock", Year: 1998, DiscNumber: 2, TrackNumber: 7}
	if err != nil || got != want {
		t.Fatalf("extended metadata=%+v err=%v want=%+v", got, err, want)
	}
	for _, invalid := range []string{"-1", "garbage", "99999999999999999999999999", "0"} {
		got, err := parseVorbisTags(vorbisComments("YEAR="+invalid, "DISCNUMBER="+invalid, "TRACKNUMBER="+invalid))
		if err != nil || got != (Tags{}) {
			t.Fatalf("invalid numeric metadata %q: %+v %v", invalid, got, err)
		}
	}
}

func TestExtendedWAVMetadata(t *testing.T) {
	var info []byte
	for _, field := range [][2]string{{"ICRD", "2003-06-01\x00"}, {"ITRK", "4/15"}, {"IMUS", "Bach, Johann Sebastian"}, {"IMUS", "Mozart"}} {
		info = append(info, wavChunk(field[0], []byte(field[1]))...)
	}
	var got Tags
	if err := got.parseWAVInfo(info); err != nil || got != (Tags{Year: 2003, TrackNumber: 4, Composer: "Bach, Johann Sebastian; Mozart"}) {
		t.Fatalf("WAV metadata=%+v err=%v", got, err)
	}
	// INFO has no standard album-artist or disc-number field; do not invent one.
	if got.AlbumArtist != "" || got.DiscNumber != 0 {
		t.Fatal("invented INFO metadata")
	}
}

func TestMultipleTagValuesStayBounded(t *testing.T) {
	var tags Tags
	for i := 0; i < 1000; i++ {
		tags.set("GENRE", strings.Repeat("x", 100)+strings.Repeat("a", i))
		tags.set("COMPOSER", strings.Repeat("y", 100)+strings.Repeat("a", i))
	}
	if len(tags.Genre) > maxTagValueBytes || len(tags.Composer) > maxTagValueBytes {
		t.Fatal("combined tags exceeded limit")
	}
}

func FuzzReadTags(f *testing.F) {
	f.Add(append(flacHead(), flacBlock(4, true, vorbisComments("TITLE=Song", "GENRE=Jazz"))...))
	f.Add(taggedWAV(wavChunk("LIST", append([]byte("INFO"), wavChunk("IGNR", []byte("Jazz"))...))))
	f.Add([]byte("fLaC\x84\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 2*maxTagBytes {
			t.Skip()
		}
		tags, _ := readTags(bytes.NewReader(b), int64(len(b)))
		for _, v := range []string{tags.Title, tags.Artist, tags.Album, tags.Genre, tags.AlbumArtist, tags.Composer} {
			if len(v) > maxTagValueBytes || !utf8.ValidString(v) {
				t.Fatalf("unbounded or invalid tag: %d bytes", len(v))
			}
		}
	})
}
