// Package state persists the agent's local state (node identity and installed-app
// inventory) as a JSON file, so a reboot or agent restart preserves registration
// and configuration without re-registering against the server.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"hifihub/agent/internal/model"
)

// Store is a mutex-guarded, file-backed state container.
type Store struct {
	mu   sync.Mutex
	path string
	s    model.State
}

// Load reads state from path, returning a fresh empty state when absent.
func Load(path string) (*Store, error) {
	st := &Store{path: path, s: model.State{Apps: map[string]model.InstalledApp{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &st.s); err != nil {
		return nil, err
	}
	if st.s.Apps == nil {
		st.s.Apps = map[string]model.InstalledApp{}
	}
	return st, nil
}

func (st *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Snapshot returns a copy of the current state.
func (st *Store) Snapshot() model.State {
	st.mu.Lock()
	defer st.mu.Unlock()
	cp := st.s
	cp.Apps = make(map[string]model.InstalledApp, len(st.s.Apps))
	for k, v := range st.s.Apps {
		cp.Apps[k] = v
	}
	return cp
}

// SetNode records the registered identity and persists it.
func (st *Store) SetNode(nodeID, alias, arch string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.NodeID, st.s.Alias, st.s.Arch, st.s.Registered = nodeID, alias, arch, true
	return st.save()
}

// SetSecret records the per-node shared secret issued at registration and
// persists it (state.json is written 0600). The secret is never logged.
func (st *Store) SetSecret(secret string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Secret = secret
	return st.save()
}

// UpsertApp inserts or updates one installed-app record and persists it.
func (st *Store) UpsertApp(a model.InstalledApp) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Apps[a.AppID] = a
	return st.save()
}

// RemoveApp deletes an installed-app record and persists it.
func (st *Store) RemoveApp(appID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.s.Apps, appID)
	return st.save()
}

// SetEnabled toggles the enabled flag for an installed app.
func (st *Store) SetEnabled(appID string, enabled bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	a, ok := st.s.Apps[appID]
	if !ok {
		return nil
	}
	a.Enabled = enabled
	st.s.Apps[appID] = a
	return st.save()
}
