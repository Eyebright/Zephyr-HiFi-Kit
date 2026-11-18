package catalog

import (
	"path/filepath"
	"testing"

	"hifihub/server/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestSeedAptApps(t *testing.T) {
	s := newSvc(t)
	if err := s.SeedAptApps(); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedAptApps(); err != nil {
		t.Fatal(err)
	}
	apps, err := s.ListAptApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 3 {
		t.Fatalf("seeded apt apps: %d", len(apps))
	}
	byID := map[string]AptApp{}
	for _, a := range apps {
		byID[a.App.AppID] = a
	}
	up, ok := byID["upmpdcli"]
	if !ok || up.Version.AptPackage != "upmpdcli" {
		t.Fatalf("upmpdcli entry: %+v", up)
	}
	if len(up.Version.DepsPkgs) != 1 || up.Version.DepsPkgs[0] != "mpd" {
		t.Fatalf("upmpdcli deps: %+v", up.Version.DepsPkgs)
	}
	// The arch-independent apt version resolves for any node arch.
	v, err := s.ResolveVersion("mpd", "apt", "arm64")
	if err != nil || v.AptPackage != "mpd" {
		t.Fatalf("resolve arm64: %v %+v", err, v)
	}
	if _, err := s.ResolveVersion("mpd", "9.9", "arm64"); err == nil {
		t.Fatal("missing version must not resolve")
	}
}