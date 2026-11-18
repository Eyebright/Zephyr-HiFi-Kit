// Package metrics collects lightweight server self-metrics for
// GET /metrics/server (version, uptime, CPU, memory, goroutines, DB size).
package metrics

import (
	"os"
	"runtime"
	"time"
)

// Version is the server build version; overridable at link time via
// -ldflags "-X hifihub/server/internal/metrics.Version=x.y.z".
var Version = "0.4.3"

// Collector snapshots process and store metrics.
type Collector struct {
	start  time.Time
	dbPath string
}

// New creates a collector for the given database path.
func New(dbPath string) *Collector {
	return &Collector{start: time.Now(), dbPath: dbPath}
}

// Snapshot returns the metrics payload required by the console.
func (c *Collector) Snapshot(signatureMode string, playGate bool) map[string]any {
	elapsed := time.Since(c.start)
	cpu, mem := processStats(elapsed)
	return map[string]any{
		"version":        Version,
		"uptime_s":       int64(elapsed.Seconds()),
		"cpu_percent":    cpu,
		"mem_mb":         mem,
		"goroutines":     runtime.NumGoroutine(),
		"db_bytes":       c.dbBytes(),
		"signature_mode": signatureMode,
		"play_gate":      playGate,
	}
}

// dbBytes sums the main DB file plus WAL/SHM sidecars when present.
func (c *Collector) dbBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(c.dbPath + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func runtimeMemMB() float64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.Sys) / (1024 * 1024)
}
