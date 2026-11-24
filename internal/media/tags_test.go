package media

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
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

func TestReadTagsAbsentAndEmpty(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty": nil,
		"short": []byte("abc"),
		"mp3":   []byte("ID3\x03\x00\x00\x00\x00\x00\x00"),
		"flac":  append([]byte("fLaC"), flacBlock(0, true, make([]byte, 34))...),
		"wav":   taggedWAV(wavChunk("LIST", []byte("INFO"))),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto)
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
			got, err := readTags(bytes.NewReader(tc.data), int64(len(tc.data)), EncodingAuto)
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
	got, err := parseVorbisTags(comment, EncodingUTF8)
	if err != nil || got != (Tags{Genre: "Soul; Rock"}) {
		t.Fatalf("Vorbis value limits: %+v, %v", got, err)
	}
	info := append(wavChunk("INAM", []byte(long)), wavChunk("IGNR", []byte("Jazz"))...)
	var tags Tags
	if err := tags.parseWAVInfo(info, EncodingAuto); err != nil || tags != (Tags{Genre: "Jazz"}) {
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
		if _, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto); err == nil {
			t.Fatal("expected metadata block limit")
		}
	}
}

func TestExtendedVorbisMetadata(t *testing.T) {
	got, err := parseVorbisTags(vorbisComments(
		"ALBUM ARTIST=王菲", "ALBUMARTIST=ignored", "COMPOSER=Bach, Johann Sebastian",
		"COMPOSER= Mozart ; Bach, Johann Sebastian", "COMPOSER=mozart", "GENRE=Jazz; Rock", "GENRE=jazz",
		"DATE=1998-05-09", "YEAR=2001", "DISCNUMBER=2/3", "TRACKNUMBER=07/12"), EncodingAuto)
	want := Tags{AlbumArtist: "王菲", Composer: "Bach, Johann Sebastian; Mozart", Genre: "Jazz; Rock", Year: 1998, DiscNumber: 2, TrackNumber: 7}
	if err != nil || got != want {
		t.Fatalf("extended metadata=%+v err=%v want=%+v", got, err, want)
	}
	for _, invalid := range []string{"-1", "garbage", "99999999999999999999999999", "0"} {
		got, err := parseVorbisTags(vorbisComments("YEAR="+invalid, "DISCNUMBER="+invalid, "TRACKNUMBER="+invalid), EncodingAuto)
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
	if err := got.parseWAVInfo(info, EncodingAuto); err != nil || got != (Tags{Year: 2003, TrackNumber: 4, Composer: "Bach, Johann Sebastian; Mozart"}) {
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

func id3Frame(version byte, id string, payload []byte) []byte {
	if version == 2 {
		n := len(payload)
		b := append([]byte(id), byte(n>>16), byte(n>>8), byte(n))
		return append(b, payload...)
	}
	var size []byte
	if version == 4 {
		size = []byte{byte(len(payload) >> 21), byte(len(payload) >> 14), byte(len(payload) >> 7), byte(len(payload))}
	} else {
		size = binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	}
	b := append([]byte(id), size...)
	b = append(b, 0, 0)
	return append(b, payload...)
}

func id3Tag(version, flags byte, frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)
	h := append([]byte("ID3"), version, 0, flags, byte(len(body)>>21), byte(len(body)>>14), byte(len(body)>>7), byte(len(body)&0x7f))
	return append(h, body...)
}

func iffChunk(id string, data []byte) []byte {
	b := append([]byte(id), binary.BigEndian.AppendUint32(nil, uint32(len(data)))...)
	b = append(b, data...)
	if len(data)%2 != 0 {
		b = append(b, 0)
	}
	return b
}

func taggedAIFF(chunks ...[]byte) []byte {
	body := append([]byte("AIFF"), bytes.Join(chunks, nil)...)
	b := append([]byte("FORM"), binary.BigEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(b, body...)
}

func taggedDSF(tag []byte) []byte {
	b := make([]byte, 36)
	copy(b, "DSD ")
	binary.LittleEndian.PutUint64(b[20:28], 36)
	binary.LittleEndian.PutUint64(b[28:36], uint64(len(tag)))
	return append(b, tag...)
}

func taggedDFF(tag []byte) []byte {
	chunk := iffChunk("ID3 ", tag)
	body := append([]byte("DFF"), chunk...)
	b := append([]byte("FRM8"), binary.BigEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(b, body...)
}

func TestReadTagsID3Containers(t *testing.T) {
	tag := id3Tag(3, 0,
		id3Frame(3, "TIT2", append([]byte{3}, []byte("标签标题")...)),
		id3Frame(3, "TPE1", append([]byte{3}, []byte("表演者")...)),
		id3Frame(3, "TALB", append([]byte{3}, []byte("专辑")...)),
		id3Frame(3, "TPE2", append([]byte{3}, []byte("专辑艺术家")...)),
		id3Frame(3, "TCOM", append([]byte{3}, []byte("作曲者")...)),
		id3Frame(3, "TDRC", append([]byte{3}, []byte("2020-05-06")...)),
		id3Frame(3, "TRCK", append([]byte{3}, []byte("07/12")...)),
		id3Frame(3, "TPOS", append([]byte{3}, []byte("2/3")...)),
		id3Frame(3, "TCON", append([]byte{3}, []byte("Jazz\x00Rock")...)),
	)
	want := Tags{Title: "标签标题", Artist: "表演者", Album: "专辑", Genre: "Jazz; Rock", AlbumArtist: "专辑艺术家", Composer: "作曲者", Year: 2020, DiscNumber: 2, TrackNumber: 7}
	info := append([]byte("INFO"), wavChunk("INAM", []byte("原生标题\x00"))...)
	for name, data := range map[string][]byte{
		"mp3":  tag,
		"wav":  taggedWAV(wavChunk("LIST", info), wavChunk("id3 ", tag)),
		"dsf":  taggedDSF(tag),
		"dff":  taggedDFF(tag),
		"aiff": taggedAIFF(iffChunk("NAME", []byte("原生标题")), iffChunk("ID3 ", tag)),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto)
			if err != nil || got != want {
				t.Fatalf("tags = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestReadTagsDSFMalformedMetadataLength(t *testing.T) {
	frame := id3Frame(3, "TIT2", append([]byte{3}, []byte("Kept")...))
	tag := id3Tag(3, 0, append([]byte{0x42}, frame...))
	data := make([]byte, 36)
	copy(data, "DSD ")
	binary.LittleEndian.PutUint64(data[20:28], 36)
	binary.LittleEndian.PutUint64(data[28:36], ^uint64(0))
	data = append(data, tag...)
	got, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto)
	if err != nil || got != (Tags{Title: "Kept"}) {
		t.Fatalf("DSF metadata recovery = %+v, %v", got, err)
	}
}

func TestReadTagsID3EncodingsAndVersions(t *testing.T) {
	units := utf16.Encode([]rune("王菲\x00Faye Wong"))
	utf16Payload := []byte{1, 0xff, 0xfe}
	for _, u := range units {
		utf16Payload = binary.LittleEndian.AppendUint16(utf16Payload, u)
	}
	legacy := append([]byte{0}, []byte{0xd6, 0xd0, 0xce, 0xc4, 0xb8, 0xe8, 0xc7, 0xfa}...)
	for _, tc := range []struct {
		name     string
		data     []byte
		encoding string
		want     Tags
	}{
		{"utf16", id3Tag(4, 0, id3Frame(4, "TPE1", utf16Payload)), EncodingAuto, Tags{Artist: "王菲"}},
		{"legacy", id3Tag(3, 0, id3Frame(3, "TIT2", legacy)), EncodingGBK, Tags{Title: "中文歌曲"}},
		{"id3v22", id3Tag(2, 0, id3Frame(2, "TT2", append([]byte{3}, []byte("Old title")...)), id3Frame(2, "TP1", append([]byte{3}, []byte("Old artist")...))), EncodingAuto, Tags{Title: "Old title", Artist: "Old artist"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(tc.data), int64(len(tc.data)), tc.encoding)
			if err != nil || got != tc.want {
				t.Fatalf("tags = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestReadTagsAIFFNativeAndMP3V1(t *testing.T) {
	aiff := taggedAIFF(iffChunk("NAME", []byte("Native title")), iffChunk("AUTH", []byte("Native artist")), iffChunk("ANNO", []byte("Not an album")))
	got, err := readTags(bytes.NewReader(aiff), int64(len(aiff)), EncodingAuto)
	if err != nil || got != (Tags{Title: "Native title", Artist: "Native artist"}) {
		t.Fatalf("AIFF native tags = %+v, %v", got, err)
	}
	v1 := make([]byte, 128)
	copy(v1, "TAG")
	copy(v1[3:33], "V1 title")
	copy(v1[33:63], "V1 artist")
	copy(v1[63:93], "V1 album")
	copy(v1[93:97], "1999")
	v1[125], v1[126], v1[127] = 4, 9, 17
	got, err = readTags(bytes.NewReader(v1), int64(len(v1)), EncodingAuto)
	if err != nil || got != (Tags{Title: "V1 title", Artist: "V1 artist", Album: "V1 album", Genre: "Rock", Year: 1999, TrackNumber: 4}) {
		t.Fatalf("ID3v1 tags = %+v, %v", got, err)
	}
}

func TestReadTagsID3ZeroLengthGroupFrame(t *testing.T) {
	good := id3Frame(4, "TIT2", append([]byte{3}, []byte("Kept")...))
	bad := id3Frame(4, "TPE1", nil)
	bad[9] = 0x40
	data := id3Tag(4, 0, good, bad)
	got, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto)
	if err == nil || got != (Tags{Title: "Kept"}) {
		t.Fatalf("zero-length group frame = %+v, %v", got, err)
	}
}

func TestReadTagsEmbeddedTagsSurviveMalformedTail(t *testing.T) {
	tag := id3Tag(3, 0, id3Frame(3, "TIT2", append([]byte{3}, []byte("标签标题")...)))
	want := Tags{Title: "标签标题"}
	badTail := []byte("JUNK\xff\xff\xff\xff")
	for name, data := range map[string][]byte{
		"wav":  taggedWAV(wavChunk("id3 ", tag), badTail),
		"aiff": taggedAIFF(iffChunk("ID3 ", tag), badTail),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(data), int64(len(data)), EncodingAuto)
			if err == nil || got != want {
				t.Fatalf("tags = %+v, %v; want %+v and an error", got, err, want)
			}
		})
	}
}

func TestReadTagsContainerMalformed(t *testing.T) {
	good := id3Frame(3, "TIT2", append([]byte{3}, []byte("Kept")...))
	badFrame := append([]byte("TPE1"), 0xff, 0xff, 0xff, 0xff, 0, 0)
	badTag := id3Tag(3, 0, good, badFrame)
	oversized := []byte("ID3\x03\x00\x00\x00\x20\x00\x01")
	badDSF := taggedDSF(good)
	binary.LittleEndian.PutUint64(badDSF[20:28], ^uint64(0))
	badAIFF := append([]byte("FORM"), binary.BigEndian.AppendUint32(nil, 20)...)
	badAIFF = append(badAIFF, []byte("AIFFNAME\xff\xff\xff\xff")...)
	for _, tc := range []struct {
		name string
		data []byte
		want Tags
	}{
		{"id3 frame length", badTag, Tags{Title: "Kept"}},
		{"id3 oversized", oversized, Tags{}},
		{"wav truncated id3", taggedWAV(wavChunk("id3 ", good[:len(good)-1])), Tags{}},
		{"dsf pointer", badDSF, Tags{}},
		{"dff chunk length", append([]byte("FRM8\x00\x00\x00\x10DFFID3 \xff\xff\xff\xff"), 0, 0, 0, 0), Tags{}},
		{"aiff chunk length", badAIFF, Tags{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readTags(bytes.NewReader(tc.data), int64(len(tc.data)), EncodingAuto)
			if err == nil || got != tc.want {
				t.Fatalf("tags = %+v, %v; want %+v and an error", got, err, tc.want)
			}
		})
	}
}

func mp4Atom(id string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	b = append(b, id...)
	return append(b, body...)
}

func mp4Data(dataType uint32, value []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, dataType)
	b = append(b, 0, 0, 0, 0)
	return mp4Atom("data", b, value)
}

func taggedMP4(items ...[]byte) []byte {
	ftyp := mp4Atom("ftyp", []byte("M4A "), []byte{0, 0, 0, 0}, []byte("M4A isom"))
	meta := mp4Atom("meta", []byte{0, 0, 0, 0}, mp4Atom("ilst", items...))
	return append(ftyp, mp4Atom("moov", mp4Atom("udta", meta))...)
}

func oggPage(packets ...[]byte) []byte {
	var out []byte
	for sequence, packet := range packets {
		var lacing []byte
		for n := len(packet); n >= 255; n -= 255 {
			lacing = append(lacing, 255)
		}
		lacing = append(lacing, byte(len(packet)%255))
		headerType := byte(0)
		if sequence == 0 {
			headerType = 2
		}
		b := append([]byte("OggS"), 0, headerType)
		b = binary.LittleEndian.AppendUint64(b, 0)
		b = binary.LittleEndian.AppendUint32(b, 1)
		b = binary.LittleEndian.AppendUint32(b, uint32(sequence))
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = append(b, byte(len(lacing)))
		b = append(b, lacing...)
		b = append(b, packet...)
		out = append(out, b...)
	}
	return out
}

func TestReadTagsMP4AndOgg(t *testing.T) {
	want := Tags{Title: "标签标题", Artist: "表演者", Album: "专辑", Genre: "Jazz; Rock", AlbumArtist: "专辑艺术家", Composer: "作曲者", Year: 2020, DiscNumber: 2, TrackNumber: 7}
	textItem := func(id, value string) []byte { return mp4Atom(id, mp4Data(1, []byte(value))) }
	mp4 := taggedMP4(
		textItem("\xa9nam", want.Title), textItem("\xa9ART", want.Artist), textItem("\xa9alb", want.Album),
		textItem("aART", want.AlbumArtist), textItem("\xa9wrt", want.Composer), textItem("\xa9day", "2020-05-06"),
		mp4Atom("\xa9gen", mp4Data(1, []byte("Jazz")), mp4Data(1, []byte("Rock"))),
		mp4Atom("trkn", mp4Data(0, []byte{0, 0, 0, 7, 0, 12, 0, 0})),
		mp4Atom("disk", mp4Data(0, []byte{0, 0, 0, 2, 0, 3, 0, 0})),
	)
	vorbisID := append([]byte("\x01vorbis"), 0, 0, 0, 0, 1)
	vorbisID = binary.LittleEndian.AppendUint32(vorbisID, 44100)
	vorbisID = append(vorbisID, make([]byte, 12)...)
	vorbisID = append(vorbisID, 0x11, 1)
	opusID := append([]byte("OpusHead"), 1, 1, 0, 0)
	opusID = binary.LittleEndian.AppendUint32(opusID, 48000)
	opusID = append(opusID, 0, 0, 0)
	comments := vorbisComments("TITLE=标签标题", "ARTIST=表演者", "ALBUM=专辑", "ALBUMARTIST=专辑艺术家", "COMPOSER=作曲者", "DATE=2020-05-06", "GENRE=Jazz", "GENRE=Rock", "DISCNUMBER=2/3", "TRACKNUMBER=7/12")
	vorbisCommentsPacket := append([]byte("\x03vorbis"), comments...)
	vorbisCommentsPacket = append(vorbisCommentsPacket, 1)
	opusCommentsPacket := append([]byte("OpusTags"), comments...)
	for name, data := range map[string][]byte{
		"m4a":       mp4,
		"vorbis":    oggPage(vorbisID, vorbisCommentsPacket, []byte("\x05vorbis\x01")),
		"opus":      oggPage(opusID, opusCommentsPacket),
		"empty_mp4": taggedMP4(),
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "tags."+name)
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadTagsWithEncoding(p, EncodingAuto)
			expected := want
			if name == "empty_mp4" {
				expected = Tags{}
			}
			if err != nil || got != expected {
				t.Fatalf("tags = %+v, %v; want %+v", got, err, expected)
			}
		})
	}
}

func FuzzReadTags(f *testing.F) {
	f.Add(append(flacHead(), flacBlock(4, true, vorbisComments("TITLE=Song", "GENRE=Jazz"))...))
	f.Add(taggedWAV(wavChunk("LIST", append([]byte("INFO"), wavChunk("IGNR", []byte("Jazz"))...))))
	f.Add([]byte("fLaC\x84\xff\xff\xff"))
	f.Add(id3Tag(3, 0, id3Frame(3, "TIT2", append([]byte{3}, []byte("Song")...))))
	f.Add(taggedDSF(id3Tag(3, 0, id3Frame(3, "TALB", append([]byte{3}, []byte("Album")...)))))
	f.Add(taggedDFF(id3Tag(3, 0, id3Frame(3, "TPE1", append([]byte{3}, []byte("Artist")...)))))
	f.Add(taggedAIFF(iffChunk("NAME", []byte("Title")), iffChunk("ID3 ", id3Tag(3, 0))))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 2*maxTagBytes {
			t.Skip()
		}
		tags, _ := readTags(bytes.NewReader(b), int64(len(b)), EncodingAuto)
		for _, v := range []string{tags.Title, tags.Artist, tags.Album, tags.Genre, tags.AlbumArtist, tags.Composer} {
			if len(v) > maxTagValueBytes || !utf8.ValidString(v) {
				t.Fatalf("unbounded or invalid tag: %d bytes", len(v))
			}
		}
	})
}
