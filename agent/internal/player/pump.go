package player

import (
	"io"

	"hifihub/zat"
)

// PumpAudio reads ZAT frames from r and writes AUDIO payloads to p. The player
// must already be opened with the negotiated format (from SETUP). It stops at
// the first non-AUDIO frame, returning that frame for the session layer to
// handle (PLAY/PAUSE/STOP/SEEK/...), or at clean end of stream (io.EOF).
//
// This is the terminal half of the vertical slice and is pure Go: r is any
// io.Reader (e.g. a TCP conn) and p is any Player (ALSA, or a fake in tests).
func PumpAudio(r io.Reader, p Player) (zat.Frame, error) {
	for {
		fr, err := zat.ReadFrame(r)
		if err == zat.ErrShortHeader {
			return zat.Frame{}, io.EOF
		}
		if err != nil {
			return zat.Frame{}, err
		}
		if fr.Type != zat.TypeAudio {
			return fr, nil
		}
		ap, err := zat.DecodeAudio(fr.Payload)
		if err != nil {
			return fr, err
		}
		if err := p.Write(ap.PCM); err != nil {
			return fr, err
		}
	}
}
