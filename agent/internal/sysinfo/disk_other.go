//go:build !linux

package sysinfo

// diskUsage is a no-op on non-Linux build platforms; the agent only runs on
// Linux terminals. This stub keeps the package cross-compilable for tests.
func diskUsage(_ string) (totalGB, usedGB, percent float64) {
	return 0, 0, 0
}
