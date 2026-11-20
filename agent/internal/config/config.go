// Package config loads terminal agent settings from environment variables and
// an optional key=value file. Defaults keep the agent usable on a fresh Pi with
// only a server URL and a one-time registration token supplied.
package config

import (
	"bufio"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings for the terminal agent.
type Config struct {
	ServerURL    string        // e.g. http://192.168.1.10:8080
	ZATAddr      string        // server ZAT source TCP addr (default: ServerURL host:7800)
	Token        string        // one-time registration token (first boot only)
	NodeID       string        // persisted after successful registration
	Alias        string        // human-friendly terminal name
	LocalBind    string        // interface the local console binds (default 0.0.0.0)
	ListenAddr   string        // local UI bind host:port (derived from LocalBind unless set)
	DataDir      string        // local state + downloaded artifacts
	Arch         string        // amd64|arm64|armhf (auto-detected when empty)
	Heartbeat    time.Duration // heartbeat interval
	Snapshot     time.Duration // full snapshot interval
	JobPoll      time.Duration // server job poll interval
	CommandPoll  time.Duration // server command poll interval
	AllowPower   bool          // expose shutdown/reboot in the local UI
	AllowLocal   bool          // allow local install/uninstall from the UI
	AgentVersion string
	BuildFlavor  string // management|audio; audio builds report ALSA playback capability
}

// BuildVersion is injected by release builds (-ldflags -X ...config.BuildVersion=x.y.z);
// a stale value makes fleet upgrades reinstall the same deb on every restart.
var BuildVersion = "0.56"

// BuildFlavor is injected for audio builds (-ldflags -X ...config.BuildFlavor=audio)
// so the server can pick the matching terminal package during upgrades.
var BuildFlavor = "management"

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		ServerURL:    "http://127.0.0.1:8080",
		LocalBind:    "0.0.0.0",
		DataDir:      "/var/lib/hifihub",
		Heartbeat:    15 * time.Second,
		Snapshot:     10 * time.Minute,
		JobPoll:      10 * time.Second,
		CommandPoll:  10 * time.Second,
		AllowPower:   true,
		AllowLocal:   true,
		AgentVersion: BuildVersion,
		BuildFlavor:  BuildFlavor,
	}
}

// Load builds a Config from defaults, then an optional file, then environment
// variables (highest precedence). The file path comes from HIFIHUB_AGENT_CONFIG
// or defaults to /etc/hifihub/agent.env.
func Load() Config {
	c := Default()
	path := os.Getenv("HIFIHUB_AGENT_CONFIG")
	if path == "" {
		path = "/etc/hifihub/agent.env"
	}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if ok {
				// Accept both bare keys (SERVER_URL) and systemd-style prefixed
				// keys (HIFIHUB_AGENT_SERVER_URL) so the shipped agent.env works
				// whether read directly or loaded via EnvironmentFile.
				k = strings.TrimPrefix(strings.TrimSpace(k), "HIFIHUB_AGENT_")
				apply(&c, k, strings.TrimSpace(v))
			}
		}
		f.Close()
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, "HIFIHUB_AGENT_") {
			apply(&c, strings.TrimPrefix(k, "HIFIHUB_AGENT_"), v)
		}
	}
	// Derive the console bind address from LOCAL_BIND unless LISTEN_ADDR was
	// set explicitly. The console port is fixed at 8080.
	if c.ListenAddr == "" {
		c.ListenAddr = net.JoinHostPort(c.LocalBind, "8080")
	}
	// Derive the ZAT source address from the server URL host unless set explicitly.
	if c.ZATAddr == "" {
		host := c.ServerURL
		if u, err := url.Parse(c.ServerURL); err == nil && u.Host != "" {
			host = u.Hostname()
		}
		if host != "" {
			c.ZATAddr = net.JoinHostPort(host, "7800")
		}
	}
	return c
}

func apply(c *Config, key, val string) {
	switch strings.ToUpper(key) {
	case "SERVER_URL":
		c.ServerURL = strings.TrimRight(val, "/")
	case "ZAT_ADDR":
		c.ZATAddr = val
	case "TOKEN":
		c.Token = val
	case "NODE_ID":
		c.NodeID = val
	case "ALIAS":
		c.Alias = val
	case "LOCAL_BIND":
		if val != "" {
			c.LocalBind = val
		}
	case "LISTEN_ADDR":
		c.ListenAddr = val
	case "DATA_DIR":
		c.DataDir = val
	case "ARCH":
		c.Arch = val
	case "HEARTBEAT_SECONDS":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			c.Heartbeat = time.Duration(n) * time.Second
		}
	case "SNAPSHOT_SECONDS":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			c.Snapshot = time.Duration(n) * time.Second
		}
	case "JOB_POLL_SECONDS":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			c.JobPoll = time.Duration(n) * time.Second
		}
	case "COMMAND_POLL_SECONDS":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			c.CommandPoll = time.Duration(n) * time.Second
		}
	case "ALLOW_POWER":
		c.AllowPower = parseBool(val)
	case "ALLOW_LOCAL":
		c.AllowLocal = parseBool(val)
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
