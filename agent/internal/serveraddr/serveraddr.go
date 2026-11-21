// Package serveraddr persists the management server address learned from the
// server or from discovery so it survives agent restarts.
package serveraddr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type State struct {
	ServerURL string `json:"server_url"`
	ZATAddr   string `json:"zat_addr"`
}

func Path(dataDir string) string { return filepath.Join(dataDir, "server-url.json") }

func Load(dataDir string) (State, bool) {
	var st State
	raw, err := os.ReadFile(Path(dataDir))
	if err != nil || json.Unmarshal(raw, &st) != nil || st.ServerURL == "" {
		return State{}, false
	}
	return st, true
}

func Save(dataDir string, st State) error {
	if dataDir == "" {
		return fmt.Errorf("empty data dir")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := Path(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
