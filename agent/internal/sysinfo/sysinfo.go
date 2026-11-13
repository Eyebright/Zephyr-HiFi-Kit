// Package sysinfo collects terminal performance metrics from /proc and /sys
// using only the standard library (FR-4 / K6). All parsers accept raw text so
// they can be unit-tested against fixtures on any build platform; the Collector
// reads from a configurable root (default "/") for the same reason.
package sysinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Metrics is a point-in-time performance snapshot of the terminal.
type Metrics struct {
	CPUPercent  float64    `json:"cpu_percent"`
	Cores       int        `json:"cores"`
	MemTotalMB  float64    `json:"mem_total_mb"`
	MemUsedMB   float64    `json:"mem_used_mb"`
	MemPercent  float64    `json:"mem_percent"`
	Load        [3]float64 `json:"load"`
	TempC       float64    `json:"temp_c"`
	UptimeSec   float64    `json:"uptime_sec"`
	DiskTotalGB float64    `json:"disk_total_gb"`
	DiskUsedGB  float64    `json:"disk_used_gb"`
	DiskPercent float64    `json:"disk_percent"`
	Kernel      string     `json:"kernel"`
	Distro      string     `json:"distro"`
	Hostname    string     `json:"hostname"`
	Arch        string     `json:"arch"`
}

// Collector reads metrics from a filesystem root. prev holds the last CPU
// counters so percent usage is computed as a delta between samples.
type Collector struct {
	root     string
	diskPath string
	prevIdle uint64
	prevTot  uint64
	hasPrev  bool
}

// New creates a Collector rooted at "/" measuring disk usage of diskPath.
func New(diskPath string) *Collector {
	return &Collector{root: "/", diskPath: diskPath}
}

// NewRooted creates a Collector reading from an arbitrary root (used by tests).
func NewRooted(root, diskPath string) *Collector {
	return &Collector{root: root, diskPath: diskPath}
}

func (c *Collector) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(c.root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// Sample gathers a full metrics snapshot.
func (c *Collector) Sample() Metrics {
	m := Metrics{Cores: runtime.NumCPU(), Arch: NormalizeArch(runtime.GOARCH)}

	if idle, tot, ok := parseCPUStat(c.read("proc/stat")); ok {
		if c.hasPrev && tot > c.prevTot {
			dTot := tot - c.prevTot
			dIdle := idle - c.prevIdle
			m.CPUPercent = 100 * float64(dTot-dIdle) / float64(dTot)
		}
		c.prevIdle, c.prevTot, c.hasPrev = idle, tot, true
	}

	total, avail := parseMemInfo(c.read("proc/meminfo"))
	m.MemTotalMB = float64(total) / 1024
	used := total - avail
	if used < 0 {
		used = 0
	}
	m.MemUsedMB = float64(used) / 1024
	if total > 0 {
		m.MemPercent = 100 * float64(used) / float64(total)
	}

	m.Load = parseLoadAvg(c.read("proc/loadavg"))
	m.UptimeSec, _ = parseUptime(c.read("proc/uptime"))
	m.TempC = c.temperature()

	m.DiskTotalGB, m.DiskUsedGB, m.DiskPercent = diskUsage(c.diskPath)

	m.Kernel = strings.TrimSpace(c.read("proc/sys/kernel/osrelease"))
	m.Hostname = strings.TrimSpace(c.read("proc/sys/kernel/hostname"))
	if m.Hostname == "" {
		m.Hostname, _ = os.Hostname()
	}
	m.Distro = parseOSRelease(c.read("etc/os-release"))
	return m
}

func (c *Collector) temperature() float64 {
	// Prefer thermal zones; fall back to hwmon. Values are millidegrees C.
	var best float64
	found := false
	zones, _ := filepath.Glob(filepath.Join(c.root, "sys/class/thermal/thermal_zone*/temp"))
	for _, z := range zones {
		if v, ok := readMilliDeg(z); ok {
			if !found || v > best {
				best, found = v, true
			}
		}
	}
	if found {
		return best
	}
	hwmons, _ := filepath.Glob(filepath.Join(c.root, "sys/class/hwmon/hwmon*/temp*_input"))
	for _, h := range hwmons {
		if v, ok := readMilliDeg(h); ok {
			if !found || v > best {
				best, found = v, true
			}
		}
	}
	return best
}

func readMilliDeg(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(n) / 1000, true
}

// NormalizeArch maps a Go GOARCH to the catalog architecture label.
func NormalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "armhf"
	default:
		return goarch
	}
}

// --- pure parsers (unit-tested) ---

func parseCPUStat(content string) (idle, total uint64, ok bool) {
	line, _, _ := strings.Cut(content, "\n")
	if !strings.HasPrefix(line, "cpu ") && !strings.HasPrefix(line, "cpu\t") {
		return 0, 0, false
	}
	fields := strings.Fields(line)[1:]
	var sum uint64
	for i, f := range fields {
		n, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		sum += n
		if i == 3 { // idle is the 4th column
			idle = n
		}
		if i == 4 { // iowait counts toward idle for usage purposes
			idle += n
		}
	}
	return idle, sum, sum > 0
}

func parseMemInfo(content string) (totalKB, availKB uint64) {
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
		switch strings.TrimSpace(k) {
		case "MemTotal":
			totalKB = n
		case "MemAvailable":
			availKB = n
		}
	}
	if availKB == 0 {
		// Older kernels lack MemAvailable; approximate from free+buffers+cached.
		var free, buffers, cached uint64
		for _, line := range strings.Split(content, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			n, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
			switch strings.TrimSpace(k) {
			case "MemFree":
				free = n
			case "Buffers":
				buffers = n
			case "Cached":
				cached = n
			}
		}
		availKB = free + buffers + cached
	}
	return totalKB, availKB
}

func parseLoadAvg(content string) [3]float64 {
	var out [3]float64
	f := strings.Fields(content)
	for i := 0; i < 3 && i < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}

func parseUptime(content string) (float64, bool) {
	f := strings.Fields(content)
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return v, err == nil
}

func parseOSRelease(content string) string {
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "PRETTY_NAME" {
			return strings.Trim(strings.TrimSpace(v), "\"")
		}
	}
	return ""
}
