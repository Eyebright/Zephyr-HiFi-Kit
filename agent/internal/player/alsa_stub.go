//go:build !cgo || !linux

package player

import (
	"errors"

	"hifihub/zat"
)

// errStub is returned by the non-CGO player. Real playback needs the ALSA
// backend, built only on Linux with CGO enabled (alsa_cgo.go).
var errStub = errors.New("player: ALSA playback requires the CGO backend (linux)")

type stubPlayer struct{}

func newPlatform() Player { return stubPlayer{} }

func (stubPlayer) Open(zat.Format, int, int, string) error { return errStub }
func (stubPlayer) Write([]byte) error                      { return errStub }
func (stubPlayer) Drain() error                            { return errStub }
func (stubPlayer) Close() error                            { return nil }
