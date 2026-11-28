package decode

import (
	"fmt"
	"io"

	"hifihub/zat"
)

// FrameSink consumes one complete ZAT frame. The ZAT server supplies a sink that
// writes the whole frame atomically under the connection write mutex, so a
// control frame (e.g. PONG) can never interleave between an audio frame's header
// and payload.
type FrameSink func(zat.Frame) error

// StreamFrames reads PCM from d and hands ZAT AUDIO frames to sink until EOF.
// PTS is the running sample position within the track (the sample clock,
// PROTOCOL.md §8); seq is the first frame sequence number and increments per
// audio frame. Only whole sample frames are sent. It returns the total sample
// frames streamed.
//
// This is the server half of the vertical slice: file -> Decoder -> PCM -> ZAT.
// It is pure Go and transport-agnostic (sink decides where frames go).
func StreamFrames(sink FrameSink, d Decoder, trackID string, seq uint32) (uint64, error) {
	return StreamFramesFrom(sink, d, trackID, seq, 0)
}

// StreamFramesFrom is StreamFrames starting at sample startSample: decoded
// samples below it are discarded and the first emitted frame carries
// PTS=startSample, so a seek restart keeps the absolute sample clock. It
// returns the ending sample position.
func StreamFramesFrom(sink FrameSink, d Decoder, trackID string, seq uint32, startSample uint64) (uint64, error) {
	format := d.Format()
	bpf := format.BytesPerFrame()
	if bpf <= 0 {
		return 0, fmt.Errorf("stream: invalid PCM format %+v", format)
	}
	buf := make([]byte, zat.MaxAudioPCM)
	var pos uint64
	pts := startSample
	if startSample > 0 {
		if sk, ok := d.(interface{ SeekSample(uint64) error }); ok {
			if err := sk.SeekSample(startSample); err != nil {
				return 0, err
			}
			pos = startSample
		}
	}
	// carry holds the trailing partial frame of the previous read: dropping it
	// would shift the sample phase of every later chunk (audible as noise on
	// formats whose frame size does not divide the read buffer, e.g. 24-bit).
	carry := 0
	for {
		n, err := d.Read(buf[carry:])
		n += carry
		carry = 0
		if n > 0 {
			usable := n - n%bpf
			if usable > 0 {
				frames := uint64(usable / bpf)
				chunkStart := pos
				pos += frames
				if pos > startSample {
					off := 0
					if chunkStart < startSample {
						off = int(startSample-chunkStart) * bpf
					}
					payload := zat.AudioPayload{PTS: pts, TrackID: trackID, PCM: buf[off:usable]}
					if serr := sink(zat.AudioFrame(seq, payload)); serr != nil {
						return pts, serr
					}
					seq++
					pts += uint64((usable - off) / bpf)
				}
			}
			if carry = n - usable; carry > 0 {
				copy(buf, buf[usable:n])
			}
		}
		if err == io.EOF {
			return pts, nil
		}
		if err != nil {
			return pts, err
		}
	}
}

// Stream is the io.Writer convenience wrapper around StreamFrames: each frame is
// written whole to w. Used by tests and any transport that owns the writer.
func Stream(w io.Writer, d Decoder, trackID string, seq uint32) (uint64, error) {
	return StreamFrames(func(f zat.Frame) error { return zat.WriteFrame(w, f) }, d, trackID, seq)
}
