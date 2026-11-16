// Package core wires the terminal agent together and implements ui.Backend. It
// owns the background loops (registration, heartbeat, snapshot, server jobs) and
// delegates every lifecycle action to the shared executor, so the local UI and
// server-driven jobs behave identically and the server stays the sole source.
package core

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"hifihub/agent/internal/audio"
	"hifihub/agent/internal/commands"
	"hifihub/agent/internal/config"
	"hifihub/agent/internal/executor"
	"hifihub/agent/internal/model"
	"hifihub/agent/internal/serverclient"
	"hifihub/agent/internal/state"
	"hifihub/agent/internal/sysinfo"
	"hifihub/agent/internal/systemctrl"
	"hifihub/agent/internal/ui"
	"hifihub/agent/internal/zatclient"
)

// Core is the assembled terminal agent.
type Core struct {
	cfg       config.Config
	st        *state.Store
	sys       *sysinfo.Collector
	ctl       *systemctrl.Controller
	run       systemctrl.Runner
	ex        *executor.Executor
	cmdExec   *commands.Executor
	client    *serverclient.Client
	zat       *zatclient.Client
	arch      string
	machineID string

	policyMu sync.RWMutex
	policy   serverclient.Policy
}

// New builds the agent core from configuration.
func New(cfg config.Config) (*Core, error) {
	arch := cfg.Arch
	if arch == "" {
		arch = sysinfo.NormalizeArch(runtime.GOARCH)
	}
	st, err := state.Load(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	run := systemctrl.ExecRunner{Timeout: 300 * time.Second}
	ctl := systemctrl.New(run, cfg.AllowPower)
	client := serverclient.New(cfg.ServerURL)
	pkg := executor.DpkgPackages{Run: run}
	ex := executor.New(client, ctl, pkg, st, arch, cfg.DataDir)
	mid, _ := os.ReadFile("/etc/machine-id")
	c := &Core{
		cfg: cfg, st: st, sys: sysinfo.New(cfg.DataDir), ctl: ctl, run: run,
		ex: ex, client: client, arch: arch, machineID: strings.TrimSpace(string(mid)),
	}
	// Command executor uses a longer runner timeout for upgrades; os-check
	// wraps its own 120s context. Playback gates read live DAC + cached policy.
	cmdRun := systemctrl.ExecRunner{Timeout: 900 * time.Second}
	c.cmdExec = commands.New(cmdRun, cfg.ServerURL, c.isPlaying, c.playProtectCached)
	// agent-upgrade converges this terminal to the server's version: download the
	// pinned deb from our own server, verify sha256, install detached, restart via
	// the deb postinst. Idempotent by version so a post-restart re-poll won't loop.
	c.cmdExec.SetUpgrade(arch, cfg.AgentVersion, cfg.DataDir, client.DownloadDist)
	// ZAT sink: dials the server source, renders PCM, drives the play gate. The
	// credential getter reads live state so registration is picked up on reconnect.
	c.zat = zatclient.New(cfg.ZATAddr, cfg.AgentVersion,
		func() (string, string) { s := c.st.Snapshot(); return s.NodeID, s.Secret },
		func() []string {
			devs := c.Devices()
			names := make([]string, 0, len(devs))
			for _, d := range devs {
				names = append(names, d.HW)
			}
			return names
		})
	return c, nil
}

// State exposes the local state store (used by main for the node id).
func (c *Core) State() *state.Store { return c.st }

// Secret returns the persisted per-node secret (never logged).
func (c *Core) Secret() string { return c.st.Snapshot().Secret }

func (c *Core) setPolicy(p serverclient.Policy) {
	c.policyMu.Lock()
	c.policy = p
	c.policyMu.Unlock()
}

func (c *Core) cachedPolicy() serverclient.Policy {
	c.policyMu.RLock()
	defer c.policyMu.RUnlock()
	return c.policy
}

func (c *Core) playProtectCached() bool { return c.cachedPolicy().PlayProtect }

// isPlaying reports whether the terminal is rendering audio: either another
// process holds the DAC, or the agent's own ZAT sink is playing. Both feed the
// FR-3.10 play gate so upgrades/power actions are blocked during ZAT playback.
func (c *Core) isPlaying() bool { return len(c.DAC()) > 0 || c.zat.Playing() }

// RunZAT maintains the outbound ZAT sink connection until ctx is cancelled.
func (c *Core) RunZAT(ctx context.Context) { c.zat.Run(ctx) }

// GateState reports the cheap local gate inputs for the UI (no network calls).
func (c *Core) GateState() ui.GateState {
	return ui.GateState{
		AllowLocal:  c.cfg.AllowLocal,
		Secret:      c.Secret(),
		Playing:     c.isPlaying(),
		PlayProtect: c.playProtectCached(),
	}
}

// --- ui.Backend implementation ---

// Identity reports terminal identity and link health.
func (c *Core) Identity(ctx context.Context) ui.Identity {
	s := c.st.Snapshot()
	alias := s.Alias
	if alias == "" {
		alias = c.cfg.Alias
	}
	p := c.cachedPolicy()
	arb := p.DacArbitration
	if arb == "" {
		arb = "auto"
	}
	return ui.Identity{
		NodeID: s.NodeID, Alias: alias, IP: localIP(), Arch: c.arch, Registered: s.Registered,
		ServerURL: c.cfg.ServerURL, ServerReachable: c.client.Health(ctx) == nil,
		SystemAvailable: c.ctl.Available(), AllowPower: c.cfg.AllowPower,
		AllowLocal: c.cfg.AllowLocal, AgentVersion: c.cfg.AgentVersion,
		Playing: c.isPlaying(), PlayProtect: p.PlayProtect, DacArbitration: arb,
	}
}

// localIP reports the primary outbound IPv4 address. The UDP dial sends no
// packets; it only selects the route. Falls back to the first non-loopback
// interface address when no default route exists.
func localIP() string {
	if conn, err := net.Dial("udp4", "8.8.8.8:53"); err == nil {
		defer conn.Close()
		if a, ok := conn.LocalAddr().(*net.UDPAddr); ok && !a.IP.IsUnspecified() {
			return a.IP.String()
		}
	}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				return ipn.IP.String()
			}
		}
	}
	return ""
}

// Services builds the service cards from local inventory, systemd state and a
// live process scan. Primary services always render; extra audio-protocol
// services render only when detected locally and not managed by the server.
func (c *Core) Services(ctx context.Context) []ui.ServiceStatus {
	apps := c.st.Snapshot().Apps
	procs := audio.ServiceProcesses(audio.RealProc{})
	var out []ui.ServiceStatus
	for _, def := range model.PrimaryServices() {
		out = append(out, c.serviceStatus(def, apps, procs))
	}
	for _, def := range model.AutoServices() {
		ss := c.serviceStatus(def, apps, procs)
		// Server-managed helpers stay hidden behind their primary's card; a
		// server-installed standalone renderer still gets its own card.
		if ss.Installed && (ss.Source != "server" || def.HelperFor == "") {
			out = append(out, ss)
		}
	}
	return out
}

// serviceStatus resolves one service card: server inventory first, then the
// systemd unit, then live processes (which also prove existence for services
// started outside systemd, e.g. moode's own shairport-sync).
func (c *Core) serviceStatus(def model.ServiceDef, apps map[string]model.InstalledApp, procs map[string][]audio.ServiceProc) ui.ServiceStatus {
	ss := ui.ServiceStatus{
		AppID: def.AppID, Name: def.Name, Protocol: def.Protocol, Category: def.Category,
		Description: def.Description, Primary: true, OccupiesDAC: def.OccupiesDAC,
		Helpers: model.HelpersFor(def.AppID), ConfigFields: def.ConfigFields,
	}
	if a, ok := apps[def.AppID]; ok {
		ss.Installed = true
		ss.Version = a.Version
		ss.Enabled = a.Enabled
		ss.Config = a.Config
		ss.Source = "server"
	}
	if us, err := c.ctl.Status(def.Units[0]); err == nil {
		ss.Running = us.Running()
		ss.ActiveState = us.ActiveState
		ss.SubState = us.SubState
		if us.Enabled {
			ss.Enabled = true
		}
		// A unit file present on the system proves the service is installed
		// even when the agent never installed it (pre-existing terminal image).
		if us.LoadState == "loaded" {
			ss.Unit = def.Units[0]
			if !ss.Installed {
				ss.Installed = true
				ss.Source = "system"
			}
		}
	}
	if hs := procs[def.AppID]; len(hs) > 0 {
		ss.Running = true
		ss.Process = hs[0].Comm
		for _, h := range hs {
			ss.PIDs = append(ss.PIDs, h.PID)
		}
		if !ss.Installed {
			ss.Installed = true
			ss.Source = "system"
		}
	}
	return ss
}

// Metrics returns a performance snapshot.
func (c *Core) Metrics() sysinfo.Metrics { return c.sys.Sample() }

// DAC returns current PCM playback holders.
func (c *Core) DAC() []audio.Holder { return audio.DetectHolders(audio.RealProc{}) }

// Devices enumerates ALSA playback devices.
func (c *Core) Devices() []audio.ALSADevice {
	devs, _ := audio.ListDevices(c.run)
	return devs
}

// Catalog lists installable apps and versions from the server.
func (c *Core) Catalog(ctx context.Context) ([]ui.CatalogEntry, error) {
	apps, err := c.client.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	installable := map[string]bool{}
	for _, d := range model.PrimaryServices() {
		installable[d.AppID] = true
	}
	for _, d := range model.AutoServices() {
		if d.HelperFor == "" {
			installable[d.AppID] = true
		}
	}
	var out []ui.CatalogEntry
	for _, a := range apps {
		if !installable[a.AppID] {
			continue
		}
		vs, err := c.client.ListVersions(ctx, a.AppID)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		var versions []string
		for _, v := range vs {
			if v.Arch != c.arch || v.Banned || seen[v.Version] {
				continue
			}
			seen[v.Version] = true
			versions = append(versions, v.Version)
		}
		out = append(out, ui.CatalogEntry{AppID: a.AppID, Name: a.Name, Versions: versions})
	}
	if out == nil {
		out = []ui.CatalogEntry{}
	}
	return out, nil
}

// Install installs an app from the server, applying DAC arbitration first when
// the app occupies the DAC.
func (c *Core) Install(ctx context.Context, appID, version string, cfg map[string]string) error {
	if err := c.arbitrateDAC(appID); err != nil {
		return err
	}
	return c.ex.Install(ctx, appID, version, cfg)
}

// appForProcess maps a DAC-holder process name to its primary app id.
func appForProcess(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "shairport"):
		return "shairport-sync"
	case strings.Contains(n, "roon"):
		return "roonbridge"
	case strings.Contains(n, "networkaudiod"):
		return "networkaudiod"
	case strings.Contains(n, "upmpdcli"), strings.Contains(n, "mpd"):
		return "upmpdcli"
	default:
		return ""
	}
}

// arbitrateDAC enforces the cached dac_arbitration policy (fallback "auto")
// before installing an app that occupies the DAC. The decision is logged.
func (c *Core) arbitrateDAC(appID string) error {
	def, ok := model.Registry[appID]
	if !ok || !def.OccupiesDAC {
		return nil
	}
	policy := c.cachedPolicy().DacArbitration
	if policy == "" {
		policy = "auto"
	}
	holders := c.DAC()
	playing := len(holders) > 0
	holderApp := ""
	if playing {
		holderApp = appForProcess(holders[0].Process)
	}
	switch policy {
	case "manual":
		if playing && holderApp != "" && holderApp != appID {
			hd := model.Registry[holderApp]
			for _, u := range hd.Units {
				_ = c.ctl.Stop(u)
				_ = c.ctl.Disable(u)
			}
			_ = c.st.SetEnabled(holderApp, false)
			log.Printf("DAC arbitration manual: stopped+disabled holder %s before installing %s", holderApp, appID)
		} else {
			log.Printf("DAC arbitration manual: no conflicting holder, installing %s", appID)
		}
	case "exclusive":
		for id, a := range c.st.Snapshot().Apps {
			if id == appID {
				continue
			}
			d := model.Registry[id]
			if d.OccupiesDAC && a.Enabled {
				log.Printf("DAC arbitration exclusive: refuse %s, %s already installed+enabled", appID, id)
				return fmt.Errorf("独占模式：%s 已安装并启用并占用 DAC，无法安装 %s。请先卸载或停用 %s。", d.Name, def.Name, d.Name)
			}
		}
		log.Printf("DAC arbitration exclusive: no other DAC app enabled, installing %s", appID)
	default: // auto
		if playing {
			name := def.Name
			holderName := holderApp
			if holderName != "" {
				holderName = model.Registry[holderApp].Name
			} else {
				holderName = holders[0].Process
			}
			log.Printf("DAC arbitration auto: refuse %s while %s is playing", appID, holderApp)
			return fmt.Errorf("DAC 正被 %s 占用播放，自动模式下无法安装 %s。请先停止播放再安装。", holderName, name)
		}
		log.Printf("DAC arbitration auto: DAC idle, installing %s", appID)
	}
	return nil
}

// Uninstall removes an app.
func (c *Core) Uninstall(ctx context.Context, appID string) error { return c.ex.Uninstall(ctx, appID) }

// Enable enables an app.
func (c *Core) Enable(appID string) error { return c.ex.Enable(appID) }

// Disable disables an app.
func (c *Core) Disable(appID string) error { return c.ex.Disable(appID) }

// Configure re-renders config and restarts an app.
func (c *Core) Configure(appID string, cfg map[string]string) error {
	return c.ex.Configure(appID, cfg)
}

// Power reboots or shuts down the terminal.
func (c *Core) Power(action string) error {
	if action == "reboot" {
		return c.ctl.Reboot()
	}
	return c.ctl.Shutdown()
}

// --- background loops ---

// Register consumes the one-time token on first boot.
func (c *Core) Register(ctx context.Context) error {
	s := c.st.Snapshot()
	if s.Registered || c.cfg.Token == "" {
		return nil
	}
	m := c.sys.Sample()
	alias := c.cfg.Alias
	if alias == "" {
		alias = m.Hostname
	}
	n, err := c.client.Register(ctx, c.cfg.Token, alias, c.arch, m.Distro, m.Kernel, c.machineID)
	if err != nil {
		return err
	}
	if err := c.st.SetNode(n.NodeID, n.Alias, c.arch); err != nil {
		return err
	}
	if n.Secret != "" {
		if err := c.st.SetSecret(n.Secret); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) protoForProcess(name string) string {
	switch {
	case strings.Contains(name, "shairport"):
		return "airplay"
	case strings.Contains(name, "roon"):
		return "raat"
	case strings.Contains(name, "networkaudiod"):
		return "naa"
	case strings.Contains(name, "upmpdcli"):
		return "upnp"
	case strings.Contains(name, "mpd"):
		// MPD holds the DAC both for UPnP/DLNA casting (via upmpdcli) and for
		// local playback; only the former is a network renderer protocol.
		if len(audio.ServiceProcesses(audio.RealProc{})["upmpdcli"]) > 0 {
			return "upnp"
		}
		return "mpd"
	default:
		return ""
	}
}

// Heartbeat reports liveness, playback and resource usage.
func (c *Core) Heartbeat(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	m := c.sys.Sample()
	holders := c.DAC()
	playing := len(holders) > 0
	active, pid := "", 0
	if playing {
		active = c.protoForProcess(holders[0].Process)
		pid = holders[0].PID
	}
	// The agent's own ZAT sink counts as playback and owns the active protocol.
	if c.zat.Playing() {
		playing = true
		active = "zat"
	}
	p, err := c.client.Heartbeat(ctx, s.NodeID, serverclient.Heartbeat{
		AgentVersion: c.cfg.AgentVersion, Playing: playing, ActiveProto: active,
		DacHolderPID: pid, CPU: m.CPUPercent, MemMB: m.MemUsedMB, TempC: m.TempC,
	})
	if err != nil {
		return err
	}
	c.setPolicy(p)
	return nil
}

// Snapshot reports the full installed-app inventory.
func (c *Core) Snapshot(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	m := c.sys.Sample()
	var apps []serverclient.InstalledApp
	for _, a := range s.Apps {
		apps = append(apps, serverclient.InstalledApp{
			AppID: a.AppID, Version: a.Version, Package: a.Package, Unit: a.Unit,
			Enabled: a.Enabled, ConfigHash: a.ConfigHash, Source: a.Source,
		})
	}
	return c.client.SendSnapshot(ctx, s.NodeID, serverclient.Snapshot{
		Alias: s.Alias, Arch: c.arch, Distro: m.Distro, Kernel: m.Kernel, Apps: apps,
	})
}

func terminalJob(state string) bool {
	switch state {
	case "done", "failed_rolledback", "failed_unrecovered", "suspended":
		return true
	}
	return false
}

// RunJobs polls and executes server-driven provision/upgrade jobs.
func (c *Core) RunJobs(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	jobs, err := c.client.ListJobs(ctx, s.NodeID)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if terminalJob(j.State) {
			continue
		}
		j, err = c.client.AdvanceJob(ctx, j.ID)
		if err != nil {
			continue
		}
		if j.State == "downloading" {
			c.executeJob(ctx, j)
		}
	}
	return nil
}

func (c *Core) executeJob(ctx context.Context, j serverclient.Job) {
	bom, err := c.client.LatestBom(ctx, j.BomID)
	if err != nil {
		c.client.ReportPhase(ctx, j.ID, "installing", "failed", err.Error())
		return
	}
	for _, ba := range bom.Apps {
		if err := c.Install(ctx, ba.AppID, ba.Version, bom.Variables); err != nil {
			c.client.ReportPhase(ctx, j.ID, "installing", "failed", err.Error())
			return
		}
	}
	for _, ph := range []string{"downloading", "verifying", "installing", "rendering_config", "enabling_units", "validating"} {
		c.client.ReportPhase(ctx, j.ID, ph, "ok", "")
	}
}

// RunCommands polls and executes pending server commands sequentially, posting
// each result with its full log. One failing command never kills the loop:
// Execute recovers panics into err results and posting errors are logged only.
func (c *Core) RunCommands(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	cmds, err := c.client.ListCommands(ctx, s.NodeID)
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		if cmd.Status != "" && cmd.Status != "pending" {
			continue
		}
		result := c.cmdExec.Execute(ctx, cmd)
		if err := c.client.CommandResult(ctx, cmd.ID, result); err != nil {
			log.Printf("command %s result post: %v", cmd.ID, err)
		}
	}
	return nil
}

// OSCheck runs an os-check command locally (shared executor) and returns its
// result for the local console.
func (c *Core) OSCheck(ctx context.Context) serverclient.CommandResult {
	return c.cmdExec.Execute(ctx, serverclient.Command{Type: "os-check"})
}

// OSUpgrade runs an os-upgrade command locally with the given package list
// (empty means all upgradable). The FR-3.10 play gate applies in the executor.
func (c *Core) OSUpgrade(ctx context.Context, packages []string) serverclient.CommandResult {
	return c.cmdExec.Execute(ctx, serverclient.Command{
		Type:   "os-upgrade",
		Params: serverclient.CommandParams{Packages: packages},
	})
}

// Netdiag runs the network diagnostic command locally and returns its result.
func (c *Core) Netdiag(ctx context.Context) serverclient.CommandResult {
	return c.cmdExec.Execute(ctx, serverclient.Command{Type: "netdiag"})
}

// --- ZAT playback (local console) ---

// Playback returns the live ZAT now-playing state plus the local device override.
func (c *Core) Playback() ui.PlaybackState {
	s := c.zat.Snapshot()
	return ui.PlaybackState{
		Connected: s.Connected, State: s.State, Playing: s.Playing, SessionID: s.SessionID,
		TrackID: s.TrackID, Title: s.Title, Artist: s.Artist, Album: s.Album,
		DurationMS: s.DurationMS, Codec: s.Codec, Rate: s.Rate, Channels: s.Channels,
		PTS: s.PTS, BufferMS: s.BufferMS, Volume: s.Volume, ALSADevice: s.ALSADevice,
		Device: c.zat.PreferredDevice(),
	}
}

// PlaybackPause toggles local pause/resume and returns the new paused state.
func (c *Core) PlaybackPause() (bool, error) { return c.zat.TogglePause() }

// PlaybackStop drains ALSA and tears down the ZAT session.
func (c *Core) PlaybackStop() error { return c.zat.Stop() }

// PlaybackSeek skips forward to a sample position.
func (c *Core) PlaybackSeek(pts uint64) error { return c.zat.Seek(pts) }

// PlaybackVolume sets soft volume 0..100.
func (c *Core) PlaybackVolume(v int) error { return c.zat.SetVolume(v) }

// PlaybackDevice sets a local preferred ALSA device (overrides the server suggestion).
func (c *Core) PlaybackDevice(dev string) error { c.zat.SetDevice(dev); return nil }

// PlaybackQueue returns the server-side playlist (the queue lives on the server).
func (c *Core) PlaybackQueue(ctx context.Context) (serverclient.QueueState, error) {
	s := c.st.Snapshot()
	if !s.Registered {
		return serverclient.QueueState{}, fmt.Errorf("终端未注册，无服务端播放列表")
	}
	return c.client.Queue(ctx, s.NodeID)
}

// PlaybackNext plays the next queue entry via the server.
func (c *Core) PlaybackNext(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return fmt.Errorf("终端未注册，无服务端播放列表")
	}
	return c.client.QueueNext(ctx, s.NodeID)
}

// PlaybackPrev plays the previous queue entry via the server.
func (c *Core) PlaybackPrev(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return fmt.Errorf("终端未注册，无服务端播放列表")
	}
	return c.client.QueuePrev(ctx, s.NodeID)
}
