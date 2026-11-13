package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProducedDebs validates the actual packages in dist/ when present. It is
// skipped in clean checkouts where packages have not been built yet.
func TestProducedDebs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "dist", "*.deb"))
	if err != nil || len(files) == 0 {
		t.Skip("no dist/*.deb built; run debpack first")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			names, members := readAr(t, b)
			if len(names) != 3 || names[0] != "debian-binary" {
				t.Fatalf("unexpected members: %v", names)
			}
			ctrl := listTar(t, members["control.tar.gz"])
			control := string(tarContents["./control"])
			arch := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "hifihub-agent_0.1.0_"), ".deb")
			if !strings.Contains(control, "Architecture: "+arch) {
				t.Fatalf("control arch mismatch for %s:\n%s", arch, control)
			}
			data := listTar(t, members["data.tar.gz"])
			bin := data["./usr/bin/hifihub-agent"]
			if bin.Size < 1_000_000 {
				t.Fatalf("agent binary suspiciously small: %d", bin.Size)
			}
			if _, ok := data["./lib/systemd/system/hifihub-agent.service"]; !ok {
				t.Fatal("missing systemd unit")
			}
			if _, ok := ctrl["./postinst"]; !ok {
				t.Fatal("missing postinst")
			}
		})
	}
}
