// Package executor is the single code path for application lifecycle actions
// (install / uninstall / enable / disable / configure). Both the local web UI
// and the server-driven job loop call it, so the server remains the sole source
// of artifacts (FR-3) and every action is verified and recorded identically.
package executor

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"hifihub/agent/internal/audio"
	"hifihub/agent/internal/model"
	"hifihub/agent/internal/serverclient"
)

// Source supplies catalog metadata and artifacts (implemented by serverclient).
type Source interface {
	Version(ctx context.Context, appID, version, arch string) (serverclient.AppVersion, error)
	Latest(ctx context.Context, appID, arch string) (serverclient.AppVersion, error)
	Download(ctx context.Context, appID, version, arch, dest string) (string, error)
}

// Units manages systemd units (implemented by systemctrl.Controller).
type Units interface {
	Start(unit string) error
	Stop(unit string) error
	Enable(unit string) error
	Disable(unit string) error
	DaemonReload() error
}

// Packages installs/removes OS packages (implemented via dpkg/apt).
type Packages interface {
	Install(ctx context.Context, debPath string, deps []string) error
	InstallDeps(ctx context.Context, deps []string) error
	Remove(ctx context.Context, pkg string) error
}

// Inventory persists the local installed-app records (implemented by state.Store).
type Inventory interface {
	UpsertApp(a model.InstalledApp) error
	RemoveApp(appID string) error
	SetEnabled(appID string, enabled bool) error
	Snapshot() model.State
}

// Executor performs lifecycle actions.
type Executor struct {
	src        Source
	units      Units
	pkg        Packages
	inv        Inventory
	arch       string
	dataDir    string
	configRoot string // prefix for config paths ("" on a real terminal)
	optRoot    string // prefix for /opt tarball installs ("" on a real terminal)
}

// New builds an Executor.
func New(src Source, units Units, pkg Packages, inv Inventory, arch, dataDir string) *Executor {
	return &Executor{src: src, units: units, pkg: pkg, inv: inv, arch: arch, dataDir: dataDir}
}

// SetConfigRoot prefixes config file paths (used by tests to redirect /etc).
func (e *Executor) SetConfigRoot(root string) { e.configRoot = root }

// SetOptRoot prefixes /opt tarball install paths (used by tests).
func (e *Executor) SetOptRoot(root string) { e.optRoot = root }

func (e *Executor) optDir() string {
	if e.optRoot != "" {
		return e.optRoot
	}
	return "/opt"
}

// RenderConfig merges default field values with overrides and renders the
// app's built-in configuration template.
func RenderConfig(def model.ServiceDef, vars map[string]string) (string, error) {
	if def.ConfigTemplate == "" {
		return "", nil
	}
	merged := map[string]string{}
	for _, f := range def.ConfigFields {
		merged[f.Key] = f.Default
	}
	for k, v := range vars {
		if v != "" {
			merged[k] = v
		}
	}
	tmpl, err := template.New(def.AppID).Parse(def.ConfigTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, merged); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (e *Executor) writeConfig(def model.ServiceDef, vars map[string]string) (string, error) {
	body, err := RenderConfig(def, vars)
	if err != nil || body == "" {
		return "", err
	}
	path := filepath.Join(e.configRoot, filepath.FromSlash(def.ConfigPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]), nil
}

// unitsFor returns the primary unit plus any helper units.
func unitsFor(appID string) []string {
	def := model.Registry[appID]
	units := append([]string{}, def.Units...)
	for _, h := range model.HelpersFor(appID) {
		units = append(units, model.Registry[h].Units...)
	}
	return units
}

// lifecycleUnits is unitsFor restricted to server-managed helpers: enable/disable
// must not stop or reconfigure a helper the terminal image owns (e.g. moode's MPD
// behind a pre-existing upmpdcli).
func (e *Executor) lifecycleUnits(appID string) []string {
	def := model.Registry[appID]
	units := append([]string{}, def.Units...)
	apps := e.inv.Snapshot().Apps
	for _, h := range model.HelpersFor(appID) {
		if _, ok := apps[h]; ok {
			units = append(units, model.Registry[h].Units...)
		}
	}
	return units
}

// depsFor returns OS package dependencies plus helper apps (served by the
// server's apt mirror), deduplicated.
func depsFor(appID string, av serverclient.AppVersion) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, d := range av.DepsPkgs {
		add(d)
	}
	for _, d := range model.Registry[appID].DepsPkgs {
		add(d)
	}
	for _, h := range model.HelpersFor(appID) {
		add(h)
	}
	return out
}

// Install downloads, verifies, installs and enables an application. An empty
// version selects the latest available for the terminal architecture.
func (e *Executor) Install(ctx context.Context, appID, version string, vars map[string]string) error {
	def, ok := model.Registry[appID]
	if !ok {
		return fmt.Errorf("unknown app %q", appID)
	}
	var av serverclient.AppVersion
	var err error
	if version == "" {
		av, err = e.src.Latest(ctx, appID, e.arch)
	} else {
		av, err = e.src.Version(ctx, appID, version, e.arch)
	}
	if err != nil {
		return fmt.Errorf("resolve %s: %w", appID, err)
	}

	suffix := "deb"
	if av.Format == "tar.bz2" {
		suffix = "tar.bz2"
	}
	dest := filepath.Join(e.dataDir, "artifacts", appID, av.Version, av.Arch,
		fmt.Sprintf("%s_%s_%s.%s", appID, av.Version, av.Arch, suffix))
	sum, err := e.src.Download(ctx, appID, av.Version, av.Arch, dest)
	if err != nil {
		return fmt.Errorf("download %s: %w", appID, err)
	}
	if av.SHA256 != "" && sum != av.SHA256 {
		return fmt.Errorf("integrity check failed for %s %s: got %s want %s", appID, av.Version, sum, av.SHA256)
	}

	if av.Format == "tar.bz2" {
		if err := e.installTarball(ctx, def, dest, depsFor(appID, av)); err != nil {
			return fmt.Errorf("install %s: %w", appID, err)
		}
	} else if err := e.pkg.Install(ctx, dest, depsFor(appID, av)); err != nil {
		return fmt.Errorf("install %s: %w", appID, err)
	}

	hash, err := e.writeConfig(def, vars)
	if err != nil {
		return fmt.Errorf("render config %s: %w", appID, err)
	}
	// Render helper configs too (e.g. MPD output device for UPnP/DLNA).
	for _, h := range model.HelpersFor(appID) {
		if _, err := e.writeConfig(model.Registry[h], vars); err != nil {
			return fmt.Errorf("render helper config %s: %w", h, err)
		}
	}

	for _, u := range unitsFor(appID) {
		if err := e.units.Enable(u); err != nil {
			return fmt.Errorf("enable %s: %w", u, err)
		}
		if err := e.units.Start(u); err != nil {
			return fmt.Errorf("start %s: %w", u, err)
		}
	}

	merged := mergeVars(def, vars)
	if err := e.inv.UpsertApp(model.InstalledApp{
		AppID: appID, Version: av.Version, Package: appID, Unit: def.Units[0],
		Enabled: true, ConfigHash: hash, Source: "server", Config: merged,
	}); err != nil {
		return err
	}
	for _, h := range model.HelpersFor(appID) {
		hd := model.Registry[h]
		_ = e.inv.UpsertApp(model.InstalledApp{
			AppID: h, Version: av.Version, Package: h, Unit: hd.Units[0],
			Enabled: true, Source: "server",
		})
	}
	return nil
}

func mergeVars(def model.ServiceDef, vars map[string]string) map[string]string {
	merged := map[string]string{}
	for _, f := range def.ConfigFields {
		merged[f.Key] = f.Default
	}
	for k, v := range vars {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

// installTarball extracts a vendor tar.bz2 (Roon Bridge) under /opt/<appID>,
// installs OS dependencies and writes the systemd units the archive lacks.
func (e *Executor) installTarball(ctx context.Context, def model.ServiceDef, archive string, deps []string) error {
	if len(deps) > 0 {
		if err := e.pkg.InstallDeps(ctx, deps); err != nil {
			return err
		}
	}
	installDir := filepath.Join(e.optDir(), def.AppID)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	if err := extractTarBz2(archive, installDir); err != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(archive), err)
	}
	if def.UnitTemplate == "" {
		return nil
	}
	tmpl, err := template.New(def.AppID + "-unit").Parse(def.UnitTemplate)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"InstallDir": installDir}); err != nil {
		return err
	}
	for _, u := range def.Units {
		path := filepath.Join(e.configRoot, "etc", "systemd", "system", u)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return e.units.DaemonReload()
}

// extractTarBz2 unpacks a bzip2 tar archive into dir, stripping the single
// top-level directory. Only dirs and regular files are accepted; links and
// escaping paths are rejected so a vendor archive cannot write outside dir.
func extractTarBz2(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr := bzip2.NewReader(f)
	return extractTarStream(tar.NewReader(zr), dir)
}

func extractTarStream(tr *tar.Reader, dir string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(hdr.Name), "/")
		if len(parts) < 2 || parts[0] == "" {
			continue // top-level dir entry itself
		}
		rel := path.Join(parts[1:]...)
		if rel == "." {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d for %q", hdr.Typeflag, hdr.Name)
		}
	}
}

// Uninstall stops, disables and removes an application.
func (e *Executor) Uninstall(ctx context.Context, appID string) error {
	def, ok := model.Registry[appID]
	if !ok {
		return fmt.Errorf("unknown app %q", appID)
	}
	for _, u := range def.Units {
		_ = e.units.Stop(u)
		_ = e.units.Disable(u)
	}
	if def.UnitTemplate != "" {
		for _, u := range def.Units {
			_ = os.Remove(filepath.Join(e.configRoot, "etc", "systemd", "system", u))
		}
		_ = e.units.DaemonReload()
		if err := os.RemoveAll(filepath.Join(e.optDir(), appID)); err != nil {
			return fmt.Errorf("remove %s: %w", appID, err)
		}
		return e.inv.RemoveApp(appID)
	}
	if err := e.pkg.Remove(ctx, appID); err != nil {
		return fmt.Errorf("remove %s: %w", appID, err)
	}
	return e.inv.RemoveApp(appID)
}

// Enable enables and starts an installed application's units.
func (e *Executor) Enable(appID string) error {
	if _, ok := model.Registry[appID]; !ok {
		return fmt.Errorf("unknown app %q", appID)
	}
	for _, u := range e.lifecycleUnits(appID) {
		if err := e.units.Enable(u); err != nil {
			return err
		}
		if err := e.units.Start(u); err != nil {
			return err
		}
	}
	return e.inv.SetEnabled(appID, true)
}

// Disable stops and disables an installed application's units.
func (e *Executor) Disable(appID string) error {
	if _, ok := model.Registry[appID]; !ok {
		return fmt.Errorf("unknown app %q", appID)
	}
	for _, u := range e.lifecycleUnits(appID) {
		_ = e.units.Stop(u)
		_ = e.units.Disable(u)
	}
	// Services started outside systemd (e.g. moode's own shairport-sync) survive
	// the unit stop; terminate their live processes so disable actually releases them.
	e.stopProcesses(appID)
	return e.inv.SetEnabled(appID, false)
}

// stopProcesses interrupts every live process of a service, then kills any
// survivor after a grace period: moode's shairport-sync worker ignores SIGINT.
func (e *Executor) stopProcesses(appID string) {
	procs := audio.ServiceProcesses(audio.RealProc{})[appID]
	if len(procs) == 0 {
		return
	}
	for _, p := range procs {
		if proc, err := os.FindProcess(p.PID); err == nil {
			_ = proc.Signal(os.Interrupt)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	for _, p := range audio.ServiceProcesses(audio.RealProc{})[appID] {
		if proc, err := os.FindProcess(p.PID); err == nil {
			_ = proc.Signal(os.Kill)
		}
	}
}

// Configure re-renders configuration and restarts the application's units.
func (e *Executor) Configure(appID string, vars map[string]string) error {
	def, ok := model.Registry[appID]
	if !ok {
		return fmt.Errorf("unknown app %q", appID)
	}
	hash, err := e.writeConfig(def, vars)
	if err != nil {
		return err
	}
	for _, h := range model.HelpersFor(appID) {
		if _, err := e.writeConfig(model.Registry[h], vars); err != nil {
			return err
		}
	}
	for _, u := range def.Units {
		_ = e.units.Stop(u)
		if err := e.units.Start(u); err != nil {
			return err
		}
	}
	cur := e.inv.Snapshot().Apps[appID]
	cur.Config = mergeVars(def, vars)
	cur.ConfigHash = hash
	return e.inv.UpsertApp(cur)
}
