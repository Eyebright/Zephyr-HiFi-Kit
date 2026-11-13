//go:build linux

package metrics

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// processStats reads /proc for CPU and RSS. cpu_percent is the average since
// collector start: (utime+stime) ticks / 100 over wall seconds.
func processStats(elapsed time.Duration) (cpuPercent, memMB float64) {
	memMB = runtimeMemMB()
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, memMB
	}
	// Skip pid and comm (comm may contain spaces and parentheses).
	s := string(data)
	idx := strings.LastIndex(s, ")")
	if idx < 0 || idx+2 >= len(s) {
		return 0, memMB
	}
	fields := strings.Fields(s[idx+2:])
	// fields[0]=state(3); utime=field 14 -> index 11; stime=field 15 -> index 12.
	if len(fields) > 12 && elapsed.Seconds() > 0 {
		utime, err1 := strconv.ParseFloat(fields[11], 64)
		stime, err2 := strconv.ParseFloat(fields[12], 64)
		if err1 == nil && err2 == nil {
			cpu := (utime + stime) / 100 / elapsed.Seconds() * 100
			if cpu > 0 {
				cpuPercent = cpu
			}
		}
	}
	if sm, err := os.ReadFile("/proc/self/statm"); err == nil {
		f := strings.Fields(string(sm))
		if len(f) > 1 {
			if pages, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				memMB = float64(pages*int64(os.Getpagesize())) / (1024 * 1024)
			}
		}
	}
	return cpuPercent, memMB
}
