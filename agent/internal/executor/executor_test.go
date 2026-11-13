package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/agent/internal/model"
	"hifihub/agent/internal/serverclient"
)

type fakeSource struct {
	ver        serverclient.AppVersion
	payload    string
	downloaded []string
}

func (f *fakeSource) Version(_ context.Context, _, _, _ string) (serverclient.AppVersion, error) {
	return f.ver, nil
}
func (f *fakeSource) Latest(_ context.Context, _, _ string) (serverclient.AppVersion, error) {
	return f.ver, nil
}
func (f *fakeSource) Download(_ context.Context, app, ver, arch, dest string) (string, error) {
	f.downloaded = append(f.downloaded, app+"/"+ver+"/"+arch)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(f.payload), 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(f.payload))
	return hex.EncodeToString(sum[:]), nil
}

type fakeUnits struct{ calls []string }

func (f *fakeUnits) rec(v, u string)        { f.calls = append(f.calls, v+" "+u) }
func (f *fakeUnits) Start(u string) error   { f.rec("start", u); return nil }
func (f *fakeUnits) Stop(u string) error    { f.rec("stop", u); return nil }
func (f *fakeUnits) Enable(u string) error  { f.rec("enable", u); return nil }
func (f *fakeUnits) Disable(u string) error { f.rec("disable", u); return nil }

type fakePackages struct {
	installed []string
	deps      []string
	removed   []string
}

func (f *fakePackages) Install(_ context.Context, deb string, deps []string) error {
	f.installed = append(f.installed, deb)
	f.deps = deps
	return nil
}
func (f *fakePackages) Remove(_ context.Context, pkg string) error {
	f.removed = append(f.removed, pkg)
	return nil
}

type fakeInventory struct{ apps map[string]model.InstalledApp }

func newInv() *fakeInventory                                  { return &fakeInventory{apps: map[string]model.InstalledApp{}} }
func (f *fakeInventory) UpsertApp(a model.InstalledApp) error { f.apps[a.AppID] = a; return nil }
func (f *fakeInventory) RemoveApp(id string) error            { delete(f.apps, id); return nil }
func (f *fakeInventory) SetEnabled(id string, en bool) error {
	a := f.apps[id]
	a.Enabled = en
	f.apps[id] = a
	return nil
}
func (f *fakeInventory) Snapshot() model.State { return model.State{Apps: f.apps} }

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestRenderConfig(t *testing.T) {
	def := model.Registry["shairport-sync"]
	body, err := RenderConfig(def, map[string]string{"airplay_name": "客厅音箱", "dac_device": "hw:2,0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `name = "客厅音箱"`) || !strings.Contains(body, `output_device = "hw:2,0"`) {
		t.Fatalf("rendered config wrong:\n%s", body)
	}
	if !strings.Contains(body, "port = 7000") { // default applied
		t.Fatalf("default port missing:\n%s", body)
	}
}

func newExec(t *testing.T, payload, wantHash string) (*Executor, *fakeSource, *fakeUnits, *fakePackages, *fakeInventory, string) {
	t.Helper()
	root := t.TempDir()
	src := &fakeSource{
		payload: payload,
		ver: serverclient.AppVersion{
			AppID: "shairport-sync", Version: "4.3.2", Arch: "arm64",
			SHA256: wantHash, Units: []string{"shairport-sync.service"},
			DepsPkgs: []string{"libsoxr0"},
		},
	}
	units := &fakeUnits{}
	pkg := &fakePackages{}
	inv := newInv()
	e := New(src, units, pkg, inv, "arm64", filepath.Join(root, "data"))
	e.SetConfigRoot(root)
	return e, src, units, pkg, inv, root
}

func TestInstallHappyPath(t *testing.T) {
	payload := "shairport-artifact"
	e, src, units, pkg, inv, root := newExec(t, payload, sha(payload))
	err := e.Install(context.Background(), "shairport-sync", "4.3.2", map[string]string{"airplay_name": "客厅"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(src.downloaded) != 1 {
		t.Fatalf("expected 1 download, got %v", src.downloaded)
	}
	if len(pkg.installed) != 1 {
		t.Fatalf("expected package install, got %v", pkg.installed)
	}
	// helpers (nqptp, avahi-daemon) must be in deps
	depSet := strings.Join(pkg.deps, ",")
	if !strings.Contains(depSet, "nqptp") || !strings.Contains(depSet, "avahi-daemon") {
		t.Fatalf("helpers missing from deps: %v", pkg.deps)
	}
	// config written under redirected root
	cfg, err := os.ReadFile(filepath.Join(root, "etc", "shairport-sync.conf"))
	if err != nil || !strings.Contains(string(cfg), "客厅") {
		t.Fatalf("config not written: %v\n%s", err, cfg)
	}
	// units enabled+started (primary + helpers)
	joined := strings.Join(units.calls, ";")
	if !strings.Contains(joined, "enable shairport-sync.service") || !strings.Contains(joined, "start nqptp.service") {
		t.Fatalf("units not managed: %v", units.calls)
	}
	a := inv.apps["shairport-sync"]
	if !a.Enabled || a.Version != "4.3.2" || a.ConfigHash == "" || a.Source != "server" {
		t.Fatalf("inventory wrong: %+v", a)
	}
}

func TestInstallHashMismatch(t *testing.T) {
	e, _, _, pkg, inv, _ := newExec(t, "real-payload", sha("different"))
	err := e.Install(context.Background(), "shairport-sync", "4.3.2", nil)
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected integrity failure, got %v", err)
	}
	if len(pkg.installed) != 0 {
		t.Fatal("must not install on hash mismatch")
	}
	if len(inv.apps) != 0 {
		t.Fatal("must not record inventory on failure")
	}
}

func TestUninstallEnableDisableConfigure(t *testing.T) {
	payload := "x"
	e, _, units, pkg, inv, root := newExec(t, payload, sha(payload))
	if err := e.Install(context.Background(), "shairport-sync", "4.3.2", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Disable("shairport-sync"); err != nil {
		t.Fatal(err)
	}
	if inv.apps["shairport-sync"].Enabled {
		t.Fatal("disable should clear enabled")
	}
	if err := e.Enable("shairport-sync"); err != nil {
		t.Fatal(err)
	}
	if !inv.apps["shairport-sync"].Enabled {
		t.Fatal("enable should set enabled")
	}
	if err := e.Configure("shairport-sync", map[string]string{"dac_device": "hw:3,0"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, "etc", "shairport-sync.conf"))
	if !strings.Contains(string(cfg), "hw:3,0") {
		t.Fatalf("configure did not rewrite config:\n%s", cfg)
	}
	if err := e.Uninstall(context.Background(), "shairport-sync"); err != nil {
		t.Fatal(err)
	}
	if len(pkg.removed) != 1 || pkg.removed[0] != "shairport-sync" {
		t.Fatalf("remove not called: %v", pkg.removed)
	}
	if _, ok := inv.apps["shairport-sync"]; ok {
		t.Fatal("inventory should drop uninstalled app")
	}
	if !strings.Contains(strings.Join(units.calls, ";"), "disable shairport-sync.service") {
		t.Fatalf("uninstall should disable units: %v", units.calls)
	}
}
