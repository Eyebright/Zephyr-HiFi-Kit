//go:build linux

package sysinfo

import "syscall"

func diskUsage(path string) (totalGB, usedGB, percent float64) {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	free := st.Bfree * bsize
	used := total - free
	const gb = 1024 * 1024 * 1024
	totalGB = float64(total) / gb
	usedGB = float64(used) / gb
	if total > 0 {
		percent = 100 * float64(used) / float64(total)
	}
	return totalGB, usedGB, percent
}
