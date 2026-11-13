package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	idle, total, ok := parseCPUStat("cpu  100 0 50 800 20 0 0 0 0 0\ncpu0 50 0 25 400 10 0 0 0 0 0\n")
	if !ok {
		t.Fatal("expected ok")
	}
	if idle != 820 { // idle(800)+iowait(20)
		t.Fatalf("idle=%d want 820", idle)
	}
	if total != 970 {
		t.Fatalf("total=%d want 970", total)
	}
	if _, _, ok := parseCPUStat("intr 123\n"); ok {
		t.Fatal("non-cpu line must not parse")
	}
}

func TestParseMemInfo(t *testing.T) {
	total, avail := parseMemInfo("MemTotal:       2048000 kB\nMemFree:         512000 kB\nMemAvailable:   1024000 kB\n")
	if total != 2048000 || avail != 1024000 {
		t.Fatalf("total=%d avail=%d", total, avail)
	}
	// Fallback path when MemAvailable is absent.
	total, avail = parseMemInfo("MemTotal: 1000 kB\nMemFree: 200 kB\nBuffers: 100 kB\nCached: 300 kB\n")
	if total != 1000 || avail != 600 {
		t.Fatalf("fallback total=%d avail=%d", total, avail)
	}
}

func TestParseLoadAndUptime(t *testing.T) {
	l := parseLoadAvg("0.52 0.58 0.59 1/234 5678\n")
	if l[0] != 0.52 || l[2] != 0.59 {
		t.Fatalf("load=%v", l)
	}
	u, ok := parseUptime("12345.67 23456.78\n")
	if !ok || u != 12345.67 {
		t.Fatalf("uptime=%v ok=%v", u, ok)
	}
}

func TestParseOSRelease(t *testing.T) {
	got := parseOSRelease("NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n")
	if got != "Debian GNU/Linux 13 (trixie)" {
		t.Fatalf("os-release=%q", got)
	}
}

func TestNormalizeArch(t *testing.T) {
	for in, want := range map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armhf"} {
		if got := NormalizeArch(in); got != want {
			t.Fatalf("NormalizeArch(%s)=%s want %s", in, got, want)
		}
	}
}

// TestCollectorSample builds a fixture /proc and /sys tree and verifies a full
// sample, including CPU delta across two reads and thermal-zone temperature.
func TestCollectorSample(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/stat", "cpu  100 0 50 800 20 0 0 0 0 0\n")
	write("proc/meminfo", "MemTotal: 2048000 kB\nMemAvailable: 1024000 kB\n")
	write("proc/loadavg", "0.10 0.20 0.30 1/100 1\n")
	write("proc/uptime", "3600.00 7000.00\n")
	write("proc/sys/kernel/osrelease", "6.6.31-v8+\n")
	write("proc/sys/kernel/hostname", "living-pi\n")
	write("etc/os-release", "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n")
	write("sys/class/thermal/thermal_zone0/temp", "45000\n")

	c := NewRooted(root, root)
	m := c.Sample()
	if m.CPUPercent != 0 {
		t.Fatalf("first sample cpu should be 0, got %v", m.CPUPercent)
	}
	if m.MemTotalMB != 2000 || m.MemUsedMB != 1000 || m.MemPercent != 50 {
		t.Fatalf("mem=%+v", m)
	}
	if m.TempC != 45 {
		t.Fatalf("temp=%v want 45", m.TempC)
	}
	if m.Kernel != "6.6.31-v8+" || m.Hostname != "living-pi" {
		t.Fatalf("kernel/hostname=%q %q", m.Kernel, m.Hostname)
	}
	if m.Distro != "Debian GNU/Linux 13 (trixie)" {
		t.Fatalf("distro=%q", m.Distro)
	}

	// Second sample: total 970->1170 (dTot 200), idle 820->920 (dIdle 100) -> 50%.
	write("proc/stat", "cpu  150 0 100 900 20 0 0 0 0 0\n")
	m2 := c.Sample()
	if m2.CPUPercent < 49 || m2.CPUPercent > 51 {
		t.Fatalf("second sample cpu=%v want ~50", m2.CPUPercent)
	}
}
