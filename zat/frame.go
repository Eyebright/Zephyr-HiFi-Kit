// Package zat implements the Zephyr Audio Transport (ZAT) v0.1 wire protocol:
// length-prefixed frames over TCP, JSON control payloads and binary PCM audio
// payloads. It is pure Go (stdlib only) so both the server and the terminal
// agent share one implementation and cannot drift. See PROTOCOL.md.
package zat

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Wire constants (PROTOCOL.md §5).
const (
	Magic       = "ZAT1"
	Version     = byte(0x01)
	HeaderSize  = 16
	MaxPayload  = 1 << 20  // 1 MiB hard cap per frame
	MaxAudioPCM = 64 << 10 // 64 KiB PCM per audio frame
)

// Type is the message type byte (PROTOCOL.md §5.2).
type Type byte

const (
	TypeHello     Type = 0x01
	TypeHelloAck  Type = 0x02
	TypeSetup     Type = 0x03
	TypeSetupAck  Type = 0x04
	TypePlay      Type = 0x05
	TypePause     Type = 0x06
	TypeSeek      Type = 0x07
	TypeStop      Type = 0x08
	TypeVolume    Type = 0x09
	TypeStatus    Type = 0x0A
	TypeTeardown  Type = 0x0B
	TypeError     Type = 0x0C
	TypePing      Type = 0x0D
	TypePong      Type = 0x0E
	TypeAudio     Type = 0x10
	TypeTrackMeta Type = 0x11
	TypeSetDevice Type = 0x12
)

// String renders a type for logs/audit (never includes secrets).
func (t Type) String() string {
	switch t {
	case TypeHello:
		return "HELLO"
	case TypeHelloAck:
		return "HELLO_ACK"
	case TypeSetup:
		return "SETUP"
	case TypeSetupAck:
		return "SETUP_ACK"
	case TypePlay:
		return "PLAY"
	case TypePause:
		return "PAUSE"
	case TypeSeek:
		return "SEEK"
	case TypeStop:
		return "STOP"
	case TypeVolume:
		return "VOLUME"
	case TypeStatus:
		return "STATUS"
	case TypeTeardown:
		return "TEARDOWN"
	case TypeError:
		return "ERROR"
	case TypePing:
		return "PING"
	case TypePong:
		return "PONG"
	case TypeAudio:
		return "AUDIO"
	case TypeTrackMeta:
		return "TRACK_META"
	case TypeSetDevice:
		return "SET_DEVICE"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", byte(t))
	}
}

// Flags is the per-frame bit field (PROTOCOL.md §5.1).
type Flags uint16

const (
	FlagAudio   Flags = 1 << 0
	FlagTLS     Flags = 1 << 1
	FlagNeedAck Flags = 1 << 2
)

// Header is the fixed 16-byte frame header.
type Header struct {
	Version    byte
	Type       Type
	Flags      Flags
	Seq        uint32
	PayloadLen uint32
}

// Frame is a header plus its payload.
type Frame struct {
	Header
	Payload []byte
}

// Sentinel errors for framing failures.
var (
	ErrBadMagic      = errors.New("zat: bad magic")
	ErrBadVersion    = errors.New("zat: unsupported version")
	ErrPayloadTooBig = errors.New("zat: payload exceeds cap")
	ErrShortHeader   = errors.New("zat: short header")
	ErrTruncated     = errors.New("zat: truncated payload")
)

// EncodeHeader writes the 16-byte big-endian header into dst (len >= HeaderSize).
func EncodeHeader(dst []byte, h Header) {
	_ = dst[HeaderSize-1]
	copy(dst[0:4], Magic)
	dst[4] = h.Version
	dst[5] = byte(h.Type)
	binary.BigEndian.PutUint16(dst[6:8], uint16(h.Flags))
	binary.BigEndian.PutUint32(dst[8:12], h.Seq)
	binary.BigEndian.PutUint32(dst[12:16], h.PayloadLen)
}

// DecodeHeader parses a 16-byte header, validating magic/version/length.
func DecodeHeader(src []byte) (Header, error) {
	if len(src) < HeaderSize {
		return Header{}, ErrShortHeader
	}
	if string(src[0:4]) != Magic {
		return Header{}, ErrBadMagic
	}
	if src[4] != Version {
		return Header{}, ErrBadVersion
	}
	n := binary.BigEndian.Uint32(src[12:16])
	if n > MaxPayload {
		return Header{}, ErrPayloadTooBig
	}
	return Header{
		Version:    src[4],
		Type:       Type(src[5]),
		Flags:      Flags(binary.BigEndian.Uint16(src[6:8])),
		Seq:        binary.BigEndian.Uint32(src[8:12]),
		PayloadLen: n,
	}, nil
}

// ReadFrame reads one full frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var hb [HeaderSize]byte
	if _, err := io.ReadFull(r, hb[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, ErrShortHeader
		}
		return Frame{}, err
	}
	h, err := DecodeHeader(hb[:])
	if err != nil {
		return Frame{}, err
	}
	payload := make([]byte, h.PayloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, ErrTruncated
	}
	return Frame{Header: h, Payload: payload}, nil
}

// WriteFrame writes one full frame to w.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayload {
		return ErrPayloadTooBig
	}
	var hb [HeaderSize]byte
	h := f.Header
	h.Version = Version
	h.PayloadLen = uint32(len(f.Payload))
	EncodeHeader(hb[:], h)
	if _, err := w.Write(hb[:]); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

// Format is a negotiated PCM sample format (PROTOCOL.md §9).
type Format string

const (
	S16LE Format = "S16_LE"
	S24LE Format = "S24_LE" // 3-byte packed little-endian
	S32LE Format = "S32_LE"
)

// BytesPerSample returns the packed byte width of one sample.
func (f Format) BytesPerSample() int {
	switch f {
	case S16LE:
		return 2
	case S24LE:
		return 3
	case S32LE:
		return 4
	default:
		return 0
	}
}

// Valid reports whether f is a known format.
func (f Format) Valid() bool { return f.BytesPerSample() > 0 }

// FrameSamples returns the number of interleaved sample frames in pcmLen bytes.
func FrameSamples(pcmLen, channels int, f Format) int {
	bps := f.BytesPerSample()
	if bps == 0 || channels <= 0 {
		return 0
	}
	return pcmLen / (channels * bps)
}

// AudioPayload is the binary audio frame body (PROTOCOL.md §5.3).
type AudioPayload struct {
	PTS     uint64 // first sample position within the track (sample clock)
	TrackID string
	PCM     []byte // interleaved little-endian samples
}

// EncodeAudio serializes an audio payload (big-endian length fields).
func EncodeAudio(p AudioPayload) []byte {
	tid := []byte(p.TrackID)
	buf := make([]byte, 8+4+len(tid)+len(p.PCM))
	binary.BigEndian.PutUint64(buf[0:8], p.PTS)
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(tid)))
	copy(buf[12:12+len(tid)], tid)
	copy(buf[12+len(tid):], p.PCM)
	return buf
}

// DecodeAudio parses an audio payload.
func DecodeAudio(b []byte) (AudioPayload, error) {
	if len(b) < 12 {
		return AudioPayload{}, ErrTruncated
	}
	pts := binary.BigEndian.Uint64(b[0:8])
	n := binary.BigEndian.Uint32(b[8:12])
	if uint32(len(b)-12) < n {
		return AudioPayload{}, ErrTruncated
	}
	tid := string(b[12 : 12+n])
	pcm := b[12+n:]
	return AudioPayload{PTS: pts, TrackID: tid, PCM: pcm}, nil
}
