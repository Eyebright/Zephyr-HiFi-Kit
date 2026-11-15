// Package media identifies audio formats from file headers (pure Go, no CGO).
// Identification is decoupled from decoding: FLAC/WAV/MP3 are fully parsed
// (rate/channels/bits/duration); other containers are detected by magic so they
// can be catalogued even before a decoder exists. See PROTOCOL.md §9.
package media

import (
	"encoding/binary"
	"io"
	"os"
)

// ProbeResult is the identified format of one audio file.
type ProbeResult struct {
	Codec        string `json:"codec"`     // flac, pcm_s16le, mp3, aac, alac, vorbis, dsd, unknown
	Container    string `json:"container"` // flac, wav, mp3, m4a, ogg, adts, dsf, dff
	Rate         int    `json:"rate"`
	Channels     int    `json:"channels"`
	Bits         int    `json:"bits"`
	TotalSamples uint64 `json:"total_samples"`
	DurationMS   int    `json:"duration_ms"`
	Decodable    bool   `json:"decodable"` // v0.1: flac + wav only
}

// headRead is how many leading bytes ProbeFile reads for identification.
const headRead = 64 * 1024

// Probe identifies a format from a leading header buffer and the full file size.
// It is pure so it can be unit-tested with crafted headers.
func Probe(head []byte, fileSize int64) ProbeResult {
	if len(head) < 4 {
		return ProbeResult{Codec: "unknown"}
	}
	switch {
	case string(head[0:4]) == "fLaC":
		return probeFLAC(head)
	case string(head[0:4]) == "RIFF" && len(head) >= 12 && string(head[8:12]) == "WAVE":
		return probeWAV(head)
	case string(head[0:4]) == "OggS":
		return probeOGG(head)
	case string(head[0:4]) == "DSD ":
		return probeDSF(head)
	case string(head[0:4]) == "FRM8":
		return ProbeResult{Codec: "dsd", Container: "dff"}
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return probeMP4(head)
	case string(head[0:3]) == "ID3" || isMP3Sync(head, 0):
		return probeMP3(head, fileSize)
	case isADTS(head):
		return probeADTS(head)
	default:
		return ProbeResult{Codec: "unknown"}
	}
}

// ProbeFile opens path, reads its head and size, and identifies the format.
func ProbeFile(path string) (ProbeResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return ProbeResult{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ProbeResult{}, err
	}
	buf := make([]byte, headRead)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ProbeResult{}, err
	}
	return Probe(buf[:n], st.Size()), nil
}

func ms(samples uint64, rate int) int {
	if rate <= 0 {
		return 0
	}
	return int(samples * 1000 / uint64(rate))
}

// --- FLAC ---

func probeFLAC(h []byte) ProbeResult {
	r := ProbeResult{Codec: "flac", Container: "flac", Decodable: true}
	// STREAMINFO is the first metadata block: 4-byte header at offset 4.
	if len(h) < 8+18 {
		return r
	}
	blockType := h[4] & 0x7f
	length := int(h[5])<<16 | int(h[6])<<8 | int(h[7])
	if blockType != 0 || length < 18 || len(h) < 8+18 {
		return r
	}
	b := h[8:]
	rate := int(b[10])<<12 | int(b[11])<<4 | int(b[12])>>4
	r.Channels = (int(b[12]) >> 1 & 0x7) + 1
	r.Bits = ((int(b[12])&0x1)<<4 | int(b[13])>>4) + 1
	r.Rate = rate
	r.TotalSamples = uint64(b[13]&0xF)<<32 | uint64(b[14])<<24 | uint64(b[15])<<16 | uint64(b[16])<<8 | uint64(b[17])
	r.DurationMS = ms(r.TotalSamples, rate)
	return r
}

// --- WAV ---

func probeWAV(h []byte) ProbeResult {
	r := ProbeResult{Container: "wav", Decodable: true}
	var fmtSeen bool
	var dataSize uint32
	p := 12
	for p+8 <= len(h) {
		id := string(h[p : p+4])
		size := binary.LittleEndian.Uint32(h[p+4 : p+8])
		data := p + 8
		switch id {
		case "fmt ":
			if data+16 <= len(h) {
				af := binary.LittleEndian.Uint16(h[data : data+2])
				r.Channels = int(binary.LittleEndian.Uint16(h[data+2 : data+4]))
				r.Rate = int(binary.LittleEndian.Uint32(h[data+4 : data+8]))
				r.Bits = int(binary.LittleEndian.Uint16(h[data+14 : data+16]))
				r.Codec = wavCodec(af, r.Bits)
				fmtSeen = true
			}
		case "data":
			dataSize = size
		}
		// Chunks are word-aligned.
		adv := int(size)
		if adv%2 == 1 {
			adv++
		}
		p = data + adv
		if id == "data" {
			break
		}
	}
	if r.Codec == "" {
		r.Codec = "pcm"
	}
	if fmtSeen && r.Channels > 0 && r.Bits > 0 {
		bytesPerFrame := r.Channels * r.Bits / 8
		if bytesPerFrame > 0 {
			r.TotalSamples = uint64(dataSize) / uint64(bytesPerFrame)
			r.DurationMS = ms(r.TotalSamples, r.Rate)
		}
	}
	return r
}

func wavCodec(audioFormat uint16, bits int) string {
	if audioFormat == 3 && bits == 32 {
		return "pcm_f32le"
	}
	switch bits {
	case 16:
		return "pcm_s16le"
	case 24:
		return "pcm_s24le"
	case 32:
		return "pcm_s32le"
	default:
		return "pcm"
	}
}

// --- MP3 ---

var mp3Rates = map[int][3]int{
	3: {44100, 48000, 32000}, // MPEG1
	2: {22050, 24000, 16000}, // MPEG2
	0: {11025, 12000, 8000},  // MPEG2.5
}

// bitrate tables in kbps for [versionGroup][layer][index]; versionGroup 3=MPEG1, else MPEG2/2.5.
var mp3Bitrates = map[int][3][16]int{
	3: { // MPEG1
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0}, // Layer1
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},    // Layer2
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},     // Layer3
	},
	2: { // MPEG2/2.5
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	},
}

// isMP3Sync matches an MPEG audio frame sync. The layer bits must be non-zero:
// ADTS AAC shares the 0xFFF sync but uses the reserved layer 00, so this check
// keeps ADTS from being misidentified as MP3.
func isMP3Sync(h []byte, off int) bool {
	return off+1 < len(h) && h[off] == 0xFF && (h[off+1]&0xE0) == 0xE0 && (h[off+1]&0x06) != 0
}

func skipID3(h []byte) int {
	if len(h) < 10 || string(h[0:3]) != "ID3" {
		return 0
	}
	size := int(h[6]&0x7f)<<21 | int(h[7]&0x7f)<<14 | int(h[8]&0x7f)<<7 | int(h[9]&0x7f)
	off := 10 + size
	if h[5]&0x10 != 0 { // footer present
		off += 10
	}
	return off
}

func probeMP3(h []byte, fileSize int64) ProbeResult {
	r := ProbeResult{Codec: "mp3", Container: "mp3"}
	off := skipID3(h)
	for off+4 <= len(h) && !isMP3Sync(h, off) {
		off++
	}
	if off+4 > len(h) {
		return r
	}
	b1, b2, b3 := h[off+1], h[off+2], h[off+3]
	verBits := (b1 >> 3) & 0x3
	layerBits := (b1 >> 1) & 0x3
	if verBits == 1 || layerBits == 0 {
		return r
	}
	version := 3 // MPEG1
	if verBits == 2 {
		version = 2
	} else if verBits == 0 {
		version = 0
	}
	layer := 4 - int(layerBits) // 1..3
	rateIdx := (b2 >> 2) & 0x3
	if rateIdx == 3 {
		return r
	}
	r.Rate = mp3Rates[version][rateIdx]
	bitIdx := (b2 >> 4) & 0xF
	group := 3
	if version != 3 {
		group = 2
	}
	if layer < 1 || layer > 3 {
		return r
	}
	kbps := mp3Bitrates[group][layer-1][bitIdx]
	mode := (b3 >> 6) & 0x3
	if mode == 3 {
		r.Channels = 1
	} else {
		r.Channels = 2
	}
	samplesPerFrame := 1152
	if version != 3 && layer == 3 {
		samplesPerFrame = 576
	} else if layer == 1 {
		samplesPerFrame = 384
	}
	// Xing/Info VBRI header gives an exact frame count.
	if frames, ok := xingFrames(h, off, version, mode); ok {
		r.TotalSamples = uint64(frames) * uint64(samplesPerFrame)
		r.DurationMS = ms(r.TotalSamples, r.Rate)
	} else if kbps > 0 && fileSize > 0 {
		audioBytes := fileSize - int64(off)
		r.DurationMS = int(audioBytes * 8 / int64(kbps))
		r.TotalSamples = uint64(r.Rate) * uint64(r.DurationMS) / 1000
	}
	return r
}

func xingFrames(h []byte, frameOff, version int, mode uint8) (uint32, bool) {
	// Xing offset depends on MPEG version and channel mode.
	var side int
	if version == 3 {
		if mode == 3 {
			side = 17
		} else {
			side = 32
		}
	} else {
		if mode == 3 {
			side = 9
		} else {
			side = 17
		}
	}
	p := frameOff + 4 + side
	if p+12 > len(h) {
		return 0, false
	}
	tag := string(h[p : p+4])
	if tag != "Xing" && tag != "Info" {
		return 0, false
	}
	flags := binary.BigEndian.Uint32(h[p+4 : p+8])
	if flags&0x1 == 0 {
		return 0, false
	}
	return binary.BigEndian.Uint32(h[p+8 : p+12]), true
}

// --- detection-only formats (catalogued, decoded in v0.2) ---

func probeOGG(h []byte) ProbeResult {
	r := ProbeResult{Codec: "vorbis", Container: "ogg"}
	// Vorbis identification packet: 0x01 + "vorbis" within the first page.
	for i := 0; i+7 < len(h) && i < 256; i++ {
		if h[i] == 0x01 && string(h[i+1:i+7]) == "vorbis" {
			if i+16 < len(h) {
				r.Channels = int(h[i+11])
				r.Rate = int(binary.LittleEndian.Uint32(h[i+12 : i+16]))
			}
			return r
		}
	}
	return r
}

func probeDSF(h []byte) ProbeResult {
	r := ProbeResult{Codec: "dsd", Container: "dsf"}
	if len(h) >= 28 {
		r.Channels = int(binary.LittleEndian.Uint32(h[24:28]))
	}
	return r
}

func probeMP4(h []byte) ProbeResult {
	r := ProbeResult{Container: "m4a"}
	// Look for an alac sample entry magic inside the head.
	for i := 0; i+4 < len(h) && i < 4096; i++ {
		if string(h[i:i+4]) == "alac" {
			r.Codec = "alac"
			return r
		}
	}
	r.Codec = "aac"
	return r
}

func isADTS(h []byte) bool {
	return len(h) >= 2 && h[0] == 0xFF && (h[1]&0xF0) == 0xF0 && (h[1]&0x06) == 0x00
}

var adtsRates = [16]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350, 0, 0, 0}

func probeADTS(h []byte) ProbeResult {
	r := ProbeResult{Codec: "aac", Container: "adts"}
	if len(h) >= 4 {
		rateIdx := (h[2] >> 2) & 0xF
		r.Rate = adtsRates[rateIdx]
		r.Channels = int((h[2]&0x1)<<2 | (h[3] >> 6))
	}
	return r
}
