package catalog

import (
	"path/filepath"
	"testing"

	"hifihub/server/internal/domain"
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
	if up.App.Origin != "distro" {
		t.Fatalf("apt seed origin = %q, want distro", up.App.Origin)
	}
}

func TestAppOriginSeedAndReclassify(t *testing.T) {
	s := newSvc(t)
	if err := s.SeedAptApps(); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedCommercialApps(); err != nil {
		t.Fatal(err)
	}
	// Backfill simulates an upgraded catalog where origins default to self.
	if _, err := s.db.Exec(`UPDATE apps SET origin='self'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedAppOrigins(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"upmpdcli": "distro", "networkaudiod": "vendor", "roonbridge": "self"}
	for id, origin := range want {
		app, err := s.GetApp(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if string(app.Origin) != origin {
			t.Fatalf("%s origin = %q, want %q", id, app.Origin, origin)
		}
	}
	// The one-time marker must not override later admin changes.
	if _, err := s.SetAppOrigin("roonbridge", "vendor"); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedAppOrigins(); err != nil {
		t.Fatal(err)
	}
	if app, _ := s.GetApp("roonbridge"); app.Origin != "vendor" {
		t.Fatalf("reclassification overwritten: %q", app.Origin)
	}
	if _, err := s.SetAppOrigin("roonbridge", "bogus"); err == nil {
		t.Fatal("invalid origin accepted")
	}
	// New apps default to self when the caller omits an origin.
	app, err := s.CreateApp(domain.App{AppID: "custom", Name: "Custom"})
	if err != nil || app.Origin != domain.OriginSelf {
		t.Fatalf("default origin: %+v %v", app, err)
	}
}
