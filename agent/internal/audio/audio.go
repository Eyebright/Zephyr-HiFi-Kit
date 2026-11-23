// Package audio enumerates ALSA output devices and detects which process holds
// the DAC (FR-2.4.2). Occupancy is detected by scanning /proc/<pid>/fd for
// handles on /dev/snd/pcm* playback devices, avoiding a dependency on fuser/lsof.
// Parsers and the holder-detection logic are pure and unit-tested.
package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"hifihub/agent/internal/systemctrl"
)

// ALSADevice is one enumerated playback device.
type ALSADevice struct {
	Card   int    `json:"card"`
	Device int    `json:"device"`
	Name   string `json:"name"`
	HW     string `json:"hw"` // hw:<card>,<device>
}

// Holder is a process currently holding a PCM playback device.
type Holder struct {
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Device  string `json:"device"` // e.g. /dev/snd/pcmC1D0p
}

var aplayLine = regexp.MustCompile(`^card\s+(\d+):\s+(.+?),\s+device\s+(\d+):\s+(.+)$`)

// ParseAplayL parses the output of `aplay -l` into playback devices.
func ParseAplayL(out string) []ALSADevice {
	var devs []ALSADevice
	for _, line := range strings.Split(out, "\n") {
		m := aplayLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		card, _ := strconv.Atoi(m[1])
		dev, _ := strconv.Atoi(m[3])
		name := strings.TrimSpace(m[2])
		if bracket := strings.Index(name, "["); bracket >= 0 {
			name = strings.TrimSpace(name[:bracket])
		}
		devs = append(devs, ALSADevice{
			Card: card, Device: dev, Name: name,
			HW: fmt.Sprintf("hw:%d,%d", card, dev),
		})
	}
	return devs
}

// CardsSignature returns the raw /proc/asound/cards content, a cheap hotplug
// change probe; empty when procfs is unavailable.
func CardsSignature() string {
	b, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return ""
	}
	return string(b)
}

// ListDevices runs `aplay -l` and parses the result.
func ListDevices(run systemctrl.Runner) ([]ALSADevice, error) {
	out, err := run.Run(context.Background(), "aplay", "-l")
	if err != nil {
		return nil, fmt.Errorf("aplay -l: %w", err)
	}
	return ParseAplayL(out), nil
}

// ProcReader abstracts /proc access so holder detection can be tested.
type ProcReader interface {
	PIDs() ([]int, error)
	Comm(pid int) string
	FDTargets(pid int) ([]string, error)
}

// RealProc reads the live /proc filesystem.
type RealProc struct{ Root string }

func (p RealProc) root() string {
	if p.Root == "" {
		return "/proc"
	}
	return p.Root
}

// PIDs lists numeric process directories.
func (p RealProc) PIDs() ([]int, error) {
	entries, err := os.ReadDir(p.root())
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// Comm reads the process command name.
func (p RealProc) Comm(pid int) string {
	b, err := os.ReadFile(filepath.Join(p.root(), strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// FDTargets resolves every fd symlink of a process to its target path.
func (p RealProc) FDTargets(pid int) ([]string, error) {
	dir := filepath.Join(p.root(), strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if target, err := os.Readlink(filepath.Join(dir, e.Name())); err == nil {
			out = append(out, target)
		}
	}
	return out, nil
}

var pcmPlayback = regexp.MustCompile(`^/dev/snd/pcmC\d+D\d+p$`)

// DetectHolders scans processes for open PCM playback devices and returns the
// holders, deduplicated per (pid, device).
func DetectHolders(pr ProcReader) []Holder {
	pids, err := pr.PIDs()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var holders []Holder
	for _, pid := range pids {
		targets, err := pr.FDTargets(pid)
		if err != nil {
			continue
		}
		comm := pr.Comm(pid)
		for _, t := range targets {
			if !pcmPlayback.MatchString(t) {
				continue
			}
			key := strconv.Itoa(pid) + "|" + t
			if seen[key] {
				continue
			}
			seen[key] = true
			holders = append(holders, Holder{PID: pid, Process: comm, Device: t})
		}
	}
	return holders
}
