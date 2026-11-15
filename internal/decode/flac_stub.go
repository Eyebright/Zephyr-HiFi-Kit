//go:build !cgo || !linux

package decode

import (
	"fmt"

	"hifihub/server/internal/media"
)

// openFLAC is the non-CGO stub. FLAC decoding needs the libFLAC backend, built
// only on Linux with CGO enabled (flac_cgo.go). WAV remains fully decodable here.
func openFLAC(path string, _ media.ProbeResult) (Decoder, error) {
	return nil, fmt.Errorf("decode: FLAC requires the CGO libFLAC backend (linux): %s", path)
}
