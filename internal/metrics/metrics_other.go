//go:build !linux

package metrics

import "time"

// processStats falls back to runtime memory stats on non-Linux platforms;
// CPU sampling via /proc is unavailable there.
func processStats(elapsed time.Duration) (cpuPercent, memMB float64) {
	return 0, runtimeMemMB()
}
