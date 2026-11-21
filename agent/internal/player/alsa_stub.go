//go:build !cgo || (!linux && !android)

package player

import (
	"errors"

	"hifihub/zat"
)

// errStub is returned by the non-CGO player. Real playback needs a CGO backend:
// ALSA on Linux (alsa_cgo.go) or AAudio on Android (aaudio_cgo.go).
var errStub = errors.New("player: audio playback requires the CGO backend (linux/ALSA or android/AAudio)")

type stubPlayer struct{}

func newPlatform() Player { return stubPlayer{} }

func supportsBitPerfect() bool { return false }

func (stubPlayer) Open(zat.Format, int, int, string) error { return errStub }
func (stubPlayer) Write([]byte) error                      { return errStub }
func (stubPlayer) Drain() error                            { return errStub }
func (stubPlayer) Drop() error                             { return errStub }
func (stubPlayer) Close() error                            { return nil }
