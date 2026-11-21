package zat

import "encoding/json"

// ALSADevice is a named ALSA playback device. Older sinks only report the HW
// string in Caps.ALSADevices; ALSADeviceInfo carries the friendly card name
// without breaking that field.
type ALSADevice struct {
	HW   string `json:"hw"`
	Name string `json:"name,omitempty"`
}

// Caps describes an endpoint's or source's audio capabilities (PROTOCOL.md §6).
type Caps struct {
	Codecs         []string     `json:"codecs"`
	Formats        []Format     `json:"formats,omitempty"`
	Rates          []int        `json:"rates,omitempty"`
	MaxChannels    int          `json:"max_channels,omitempty"`
	ALSADevices    []string     `json:"alsa_devices,omitempty"`
	ALSADeviceInfo []ALSADevice `json:"alsa_device_info,omitempty"`
	// MQA reports bit-perfect MQA passthrough support (strict ALSA hw path).
	MQA bool `json:"mqa,omitempty"`
	// DoP reports DSD-over-PCM passthrough support (strict 24-bit hw path).
	DoP bool `json:"dop,omitempty"`
}

// Hello is sent sink→source to authenticate and declare capabilities.
type Hello struct {
	NodeID       string `json:"node_id"`
	SecretSHA256 string `json:"secret_sha256"`
	AgentVersion string `json:"agent_version"`
	Caps         Caps   `json:"caps"`
}

// HelloAck is sent source→sink with the session id and server capabilities.
type HelloAck struct {
	SessionID     string `json:"session_id"`
	ServerVersion string `json:"server_version"`
	Caps          Caps   `json:"caps"`
	PrebufferMS   int    `json:"prebuffer_ms"`
}

// Setup is sent source→sink to describe the track and negotiated PCM format.
type Setup struct {
	SessionID    string  `json:"session_id"`
	TrackID      string  `json:"track_id"`
	Format       Format  `json:"format"`
	Rate         int     `json:"rate"`
	Channels     int     `json:"channels"`
	PrebufferMS  int     `json:"prebuffer_ms"`
	ALSADevice   string  `json:"alsa_device"`
	TotalSamples uint64  `json:"total_samples"`
	GainDB       float64 `json:"gain_db"`
	// MQA asks the sink for strict bit-perfect output; software volume and
	// converting plugins are not allowed for this session.
	MQA bool `json:"mqa,omitempty"`
	// DoP marks the PCM as DSD-over-PCM; DSDRate is the source 1-bit rate.
	DoP     bool `json:"dop,omitempty"`
	DSDRate int  `json:"dsd_rate,omitempty"`
}

// OpenedParams echoes the ALSA hw params the sink actually opened.
type OpenedParams struct {
	Format   Format `json:"format"`
	Rate     int    `json:"rate"`
	Channels int    `json:"channels"`
}

// SetupAck is sent sink→source confirming the ALSA device is ready.
type SetupAck struct {
	SessionID    string       `json:"session_id"`
	OK           bool         `json:"ok"`
	Opened       OpenedParams `json:"opened"`
	BufferFrames int          `json:"buffer_frames"`
	Detail       string       `json:"detail,omitempty"`
	// BitPerfect is set when the sink opened a strict hw path without format
	// conversion, resampling or software gain.
	BitPerfect bool `json:"bit_perfect,omitempty"`
}

// Play starts or resumes playback, optionally from a sample position.
type Play struct {
	SessionID string `json:"session_id"`
	StartPTS  uint64 `json:"start_pts"`
}

// Pause holds playback (ALSA handle kept open).
type Pause struct {
	SessionID string `json:"session_id"`
}

// Seek jumps to a sample position.
type Seek struct {
	SessionID string `json:"session_id"`
	PTS       uint64 `json:"pts"`
}

// Stop ends playback and drains ALSA.
type Stop struct {
	SessionID string `json:"session_id"`
}

// Volume sets soft volume 0..100.
type Volume struct {
	SessionID string `json:"session_id"`
	Volume    int    `json:"volume"`
}

// SetDevice sets the sink's preferred ALSA output device, overriding the SETUP
// suggestion. Like the local console's device picker it applies at next SETUP.
type SetDevice struct {
	Device string `json:"device"`
}

// Status is the periodic sink→source report.
type Status struct {
	SessionID    string `json:"session_id"`
	State        string `json:"state"`
	PTS          uint64 `json:"pts"`
	BufferMS     int    `json:"buffer_ms"`
	Underruns    int    `json:"underruns"`
	ALSADevice   string `json:"alsa_device"`
	PrefDevice   string `json:"pref_device,omitempty"`
	ClockPPM     int    `json:"clock_ppm"`
	DacHolderPID int    `json:"dac_holder_pid"`
}

// TrackMeta carries now-playing metadata source→sink.
type TrackMeta struct {
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	Rate       int    `json:"rate"`
	Channels   int    `json:"channels"`
	CoverURL   string `json:"cover_url,omitempty"`
	MQA        bool   `json:"mqa,omitempty"`
	DoP        bool   `json:"dop,omitempty"`
	DSDRate    int    `json:"dsd_rate,omitempty"`
}

// Teardown closes a session.
type Teardown struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"`
}

// Error reports a protocol or playback error.
type Error struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	Fatal     bool   `json:"fatal"`
}

// Ping/Pong keepalive.
type Ping struct {
	Nonce string `json:"nonce"`
}
type Pong struct {
	Nonce string `json:"nonce"`
}

// Error codes (PROTOCOL.md §12).
const (
	ErrAuthFailed        = "auth_failed"
	ErrNotAuthorized     = "not_authorized"
	ErrRateUnsupported   = "rate_unsupported"
	ErrFormatUnsupported = "format_unsupported"
	ErrDecodeFailed      = "decode_failed"
	ErrAlsaOpenFailed    = "alsa_open_failed"
	ErrUnderrun          = "underrun"
	ErrOverrun           = "overrun"
	ErrSessionTimeout    = "session_timeout"
	ErrMountFailed       = "mount_failed"
	ErrInternal          = "internal"
)

// MarshalPayload encodes a control message to JSON.
func MarshalPayload(v any) ([]byte, error) { return json.Marshal(v) }

// UnmarshalPayload decodes a JSON control payload into v.
func UnmarshalPayload(b []byte, v any) error { return json.Unmarshal(b, v) }

// ControlFrame builds a control frame (JSON payload) with the given type/seq.
func ControlFrame(t Type, seq uint32, v any) (Frame, error) {
	p, err := json.Marshal(v)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Header: Header{Type: t, Seq: seq, PayloadLen: uint32(len(p))}, Payload: p}, nil
}

// AudioFrame builds an AUDIO frame from a payload.
func AudioFrame(seq uint32, p AudioPayload) Frame {
	body := EncodeAudio(p)
	return Frame{
		Header:  Header{Type: TypeAudio, Flags: FlagAudio, Seq: seq, PayloadLen: uint32(len(body))},
		Payload: body,
	}
}
