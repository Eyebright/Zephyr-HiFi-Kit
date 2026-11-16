// Package executor is the single code path for application lifecycle actions
// (install / uninstall / enable / disable / configure). Both the local web UI
// and the server-driven job loop call it, so the server remains the sole source
// of artifacts (FR-3) and every action is verified and recorded identically.
package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
}

// Packages installs/removes OS packages (implemented via dpkg/apt).
type Packages interface {
	Install(ctx context.Context, debPath string, deps []string) error
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
}

// New builds an Executor.
func New(src Source, units Units, pkg Packages, inv Inventory, arch, dataDir string) *Executor {
	return &Executor{src: src, units: units, pkg: pkg, inv: inv, arch: arch, dataDir: dataDir}
}

// SetConfigRoot prefixes config file paths (used by tests to redirect /etc).
func (e *Executor) SetConfigRoot(root string) { e.configRoot = root }

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

	dest := filepath.Join(e.dataDir, "artifacts", appID, av.Version, av.Arch,
		fmt.Sprintf("%s_%s_%s.deb", appID, av.Version, av.Arch))
	sum, err := e.src.Download(ctx, appID, av.Version, av.Arch, dest)
	if err != nil {
		return fmt.Errorf("download %s: %w", appID, err)
	}
	if av.SHA256 != "" && sum != av.SHA256 {
		return fmt.Errorf("integrity check failed for %s %s: got %s want %s", appID, av.Version, sum, av.SHA256)
	}

	if err := e.pkg.Install(ctx, dest, depsFor(appID, av)); err != nil {
		return fmt.Errorf("install %s: %w", appID, err)
	}

	hash, err := e.writeConfig(def, vars)
	if err != nil {
		return fmt.Errorf("render config %s: %w", appID, err)
	}
	// Render helper configs too (e.g. MPD output device for QPlay).
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
