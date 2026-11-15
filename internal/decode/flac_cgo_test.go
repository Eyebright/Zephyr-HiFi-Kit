//go:build cgo && linux

package decode

import (
	"bytes"
	"io"
	"os/exec"
	"testing"

	"hifihub/zat"
)

// TestFLACDecodeRoundTrip encodes a known PCM WAV to FLAC with the flac CLI,
// then decodes it through the CGO libFLAC backend and asserts bit-exact PCM.
// It runs in Linux CI (CGO + libflac-dev + flac); skipped if flac is absent.
func TestFLACDecodeRoundTrip(t *testing.T) {
	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("flac CLI not available; install flac to run the CGO FLAC decode test")
	}
	for _, tc := range []struct {
		format zat.Format
		bps    int
	}{
		{zat.S16LE, 2},
		{zat.S24LE, 3},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			pcm := rampPCM(20000, 2, tc.bps)
			wavPath := buildWAV(t, tc.format, 44100, 2, pcm)
			flacPath := wavPath + ".flac"
			cmd := exec.Command(flacBin, "-s", "-f", "-o", flacPath, wavPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("flac encode: %v (%s)", err, out)
			}
			d, err := Open(flacPath)
			if err != nil {
				t.Fatalf("open flac: %v", err)
			}
			defer d.Close()
			if d.Format().Format != tc.format || d.Format().Channels != 2 || d.Format().Rate != 44100 {
				t.Fatalf("format = %+v", d.Format())
			}
			got, err := io.ReadAll(d)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got, pcm) {
				t.Fatalf("FLAC round-trip mismatch: got %d bytes want %d", len(got), len(pcm))
			}
		})
	}
}
