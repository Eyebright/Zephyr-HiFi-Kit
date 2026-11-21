package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveredURLOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server-url.json"),
		[]byte(`{"server_url":"http://192.168.0.41:8080","zat_addr":"192.168.0.41:7800"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIFIHUB_AGENT_DATA_DIR", dir)
	t.Setenv("HIFIHUB_AGENT_SERVER_URL", "")
	c := Load()
	if c.ServerURL != "http://192.168.0.41:8080" || c.ZATAddr != "192.168.0.41:7800" {
		t.Fatalf("config = %+v", c)
	}
}
