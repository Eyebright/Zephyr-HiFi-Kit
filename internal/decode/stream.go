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
	format := d.Format()
	bpf := format.BytesPerFrame()
	if bpf <= 0 {
		return 0, fmt.Errorf("stream: invalid PCM format %+v", format)
	}
	buf := make([]byte, zat.MaxAudioPCM)
	var pts uint64
	for {
		n, err := d.Read(buf)
		if n > 0 {
			usable := n - n%bpf
			if usable > 0 {
				payload := zat.AudioPayload{PTS: pts, TrackID: trackID, PCM: buf[:usable]}
				if serr := sink(zat.AudioFrame(seq, payload)); serr != nil {
					return pts, serr
				}
				seq++
				pts += uint64(usable / bpf)
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
