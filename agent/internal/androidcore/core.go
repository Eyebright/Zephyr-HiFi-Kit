// Package androidcore wires the Android terminal: a pure ZAT playback sink that
// registers with the management server, reports liveness, receives ZAT audio and
// renders it through AAudio (player package, cgo && android). It reuses the same
// pure-Go zatclient/config/state/serverclient/sysinfo as the Linux agent, but
// trims the Linux-only app-installation, systemd, dpkg and DAC-arbitration surface
// that does not exist on Android (no root, no systemd, no ALSA device nodes).
package androidcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hifihub/agent/internal/config"
	"hifihub/agent/internal/serverclient"
	"hifihub/agent/internal/state"
	"hifihub/agent/internal/sysinfo"
	"hifihub/agent/internal/zatclient"
	"hifihub/zat"
)

// Version is the Android terminal agent version (kept in step with the release).
var Version = "0.53"

// Config is the JSON configuration handed over by the Kotlin shell at startup.
type Config struct {
	ServerURL  string `json:"server_url"`
	ZATAddr    string `json:"zat_addr"`
	Token      string `json:"token"`
	Alias      string `json:"alias"`
	DataDir    string `json:"data_dir"`
	AndroidID  string `json:"android_id"`
	OSVersion  string `json:"os_version"`
	ListenAddr string `json:"listen_addr"`
}

// Core is the assembled Android terminal.
type Core struct {
	cfg         config.Config
	st          *state.Store
	sys         *sysinfo.Collector
	client      *serverclient.Client
	zat         *zatclient.Client
	arch        string
	machineID   string
	osVersion   string
	consoleAddr string

	policyMu sync.RWMutex
	policy   serverclient.Policy
}

// New builds the Android core from the shell-supplied configuration.
func New(in Config) (*Core, error) {
	if in.DataDir == "" {
		in.DataDir = "."
	}
	if err := os.MkdirAll(in.DataDir, 0o755); err != nil {
		return nil, err
	}
	cfg := config.Config{
		ServerURL:    in.ServerURL,
		ZATAddr:      in.ZATAddr,
		Token:        in.Token,
		Alias:        in.Alias,
		DataDir:      in.DataDir,
		Arch:         "android",
		AgentVersion: Version,
		AllowLocal:   true,
		AllowPower:   false,
		Heartbeat:    15 * time.Second,
		Snapshot:     10 * time.Minute,
	}
	if cfg.ZATAddr == "" {
		host := cfg.ServerURL
		if u, err := url.Parse(cfg.ServerURL); err == nil && u.Host != "" {
			host = u.Hostname()
		}
		if host != "" {
			cfg.ZATAddr = net.JoinHostPort(host, "7800")
		}
	}
	st, err := state.Load(filepath.Join(in.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	mid := in.AndroidID
	if mid == "" {
		mid = persistentMachineID(in.DataDir)
	}
	consoleAddr := in.ListenAddr
	if consoleAddr == "" {
		consoleAddr = "127.0.0.1:8090"
	}
	c := &Core{
		cfg: cfg, st: st, sys: sysinfo.New(in.DataDir),
		client: serverclient.New(cfg.ServerURL), arch: "android",
		machineID: mid, osVersion: in.OSVersion, consoleAddr: consoleAddr,
	}
	c.zat = zatclient.New(cfg.ZATAddr, Version,
		func() (string, string) { s := c.st.Snapshot(); return s.NodeID, s.Secret },
		func() []zat.ALSADevice { return []zat.ALSADevice{{HW: "default"}} })
	return c, nil
}

// persistentMachineID returns a stable per-install id, generating and persisting
// one on first run. Android has no /etc/machine-id.
func persistentMachineID(dataDir string) string {
	path := filepath.Join(dataDir, "machine-id")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b)
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "android-unknown"
	}
	id := hex.EncodeToString(buf)
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}

// State exposes the local state store.
func (c *Core) State() *state.Store { return c.st }

// Secret returns the persisted per-node secret (never logged).
func (c *Core) Secret() string { return c.st.Snapshot().Secret }

// ConsoleAddr is the loopback address of the local web console.
func (c *Core) ConsoleAddr() string { return c.consoleAddr }

// Playing reports whether ZAT audio is actively rendering.
func (c *Core) Playing() bool { return c.zat.Playing() }

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

// Register consumes the one-time token on first launch.
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
	if alias == "" {
		alias = "android-terminal"
	}
	n, err := c.client.Register(ctx, c.cfg.Token, alias, c.arch, "android", c.osVersion, c.machineID)
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

// Heartbeat reports liveness, playback and resource usage.
func (c *Core) Heartbeat(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	m := c.sys.Sample()
	playing := c.zat.Playing()
	active := ""
	if playing {
		active = "zat"
	}
	p, err := c.client.Heartbeat(ctx, s.NodeID, serverclient.Heartbeat{
		AgentVersion: Version, Playing: playing, ActiveProto: active,
		CPU: m.CPUPercent, MemMB: m.MemUsedMB, TempC: m.TempC,
	})
	if err != nil {
		return err
	}
	c.setPolicy(p)
	return nil
}

// Snapshot reports identity. Android installs no server apps, so the inventory
// is empty; the server still tracks the node as an online ZAT terminal.
func (c *Core) Snapshot(ctx context.Context) error {
	s := c.st.Snapshot()
	if !s.Registered {
		return nil
	}
	m := c.sys.Sample()
	return c.client.SendSnapshot(ctx, s.NodeID, serverclient.Snapshot{
		Alias: s.Alias, Arch: c.arch, Distro: "android", Kernel: m.Kernel,
	})
}

// RunZAT maintains the outbound ZAT sink connection until ctx is cancelled.
func (c *Core) RunZAT(ctx context.Context) { c.zat.Run(ctx) }

// Run launches the background loops (registration, heartbeat, snapshot) and the
// ZAT sink. It returns when ctx is cancelled.
func (c *Core) Run(ctx context.Context) {
	if err := c.Register(ctx); err != nil {
		log.Printf("androidcore: registration pending: %v", err)
	} else if c.st.Snapshot().Registered {
		_ = c.Snapshot(ctx)
	}
	go c.loop(ctx, c.cfg.Heartbeat, func() {
		if err := c.Register(ctx); err != nil {
			return
		}
		if err := c.Heartbeat(ctx); err != nil {
			log.Printf("androidcore: heartbeat: %v", err)
		}
	})
	go c.loop(ctx, c.cfg.Snapshot, func() {
		if err := c.Snapshot(ctx); err != nil {
			log.Printf("androidcore: snapshot: %v", err)
		}
	})
	c.RunZAT(ctx)
}

func (c *Core) loop(ctx context.Context, every time.Duration, fn func()) {
	if every <= 0 {
		every = 15 * time.Second
	}
	fn()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// --- playback controls (local console + JNI) ---

// Playback returns the live ZAT now-playing snapshot.
func (c *Core) Playback() zatclient.Snapshot { return c.zat.Snapshot() }

// Pause toggles local pause/resume, returning the new paused state.
func (c *Core) Pause() (bool, error) { return c.zat.TogglePause() }

// Stop drains AAudio and tears down the ZAT session.
func (c *Core) Stop() error { return c.zat.Stop() }

// Seek skips forward to a sample position.
func (c *Core) Seek(pts uint64) error { return c.zat.Seek(pts) }

// Volume sets soft volume 0..100.
func (c *Core) Volume(v int) error { return c.zat.SetVolume(v) }

// Device sets a local preferred output (Android uses the AAudio default).
func (c *Core) Device(dev string) { c.zat.SetDevice(dev) }

// PreferredDevice returns the local output override.
func (c *Core) PreferredDevice() string { return c.zat.PreferredDevice() }

// Metrics returns a performance snapshot with the Android distro label.
func (c *Core) Metrics() sysinfo.Metrics {
	m := c.sys.Sample()
	m.Distro = "android"
	m.Arch = c.arch
	return m
}

// Identity is the focused node identity for the Android console.
type Identity struct {
	NodeID          string `json:"node_id"`
	Alias           string `json:"alias"`
	Arch            string `json:"arch"`
	Registered      bool   `json:"registered"`
	ServerURL       string `json:"server_url"`
	ServerReachable bool   `json:"server_reachable"`
	AgentVersion    string `json:"agent_version"`
	OSVersion       string `json:"os_version"`
	Playing         bool   `json:"playing"`
	PlayProtect     bool   `json:"play_protect"`
}

// Identity reports terminal identity and link health.
func (c *Core) Identity(ctx context.Context) Identity {
	s := c.st.Snapshot()
	alias := s.Alias
	if alias == "" {
		alias = c.cfg.Alias
	}
	return Identity{
		NodeID: s.NodeID, Alias: alias, Arch: c.arch, Registered: s.Registered,
		ServerURL: c.cfg.ServerURL, ServerReachable: c.client.Health(ctx) == nil,
		AgentVersion: Version, OSVersion: c.osVersion, Playing: c.Playing(),
		PlayProtect: c.cachedPolicy().PlayProtect,
	}
}
