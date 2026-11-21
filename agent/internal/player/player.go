// Package player renders ZAT PCM audio to a terminal's output device. The ALSA
// backend is CGO/Linux (alsa_cgo.go); elsewhere a stub keeps non-CGO builds and
// tests green (alsa_stub.go). PumpAudio is the pure-Go receive half of the
// vertical slice: ZAT frames -> PCM -> Player. See PROTOCOL.md.
package player

import "hifihub/zat"

// Player renders interleaved little-endian PCM to an audio device.
type Player interface {
	// Open configures the device for the negotiated PCM format. device is an
	// ALSA name such as "hw:0,0", "default" or "null" (CI loopback).
	Open(format zat.Format, rate, channels int, device string) error
	// Write renders a buffer of interleaved PCM, blocking until consumed.
	Write(pcm []byte) error
	// Drain flushes pending samples to the device.
	Drain() error
	// Drop discards pending samples immediately (seek / track change).
	Drop() error
	// Close releases the device.
	Close() error
}

// BitPerfectPlayer opens a strict hardware path for MQA passthrough. The
// implementation must reject converting plugins, resampling and gain.
type BitPerfectPlayer interface {
	OpenBitPerfect(format zat.Format, rate, channels int, device string) error
}

// SupportsBitPerfect reports whether this build can deliver a strict MQA path.
func SupportsBitPerfect() bool { return supportsBitPerfect() }

// New returns the platform player: ALSA on Linux with CGO, a stub elsewhere.
func New() Player { return newPlatform() }
