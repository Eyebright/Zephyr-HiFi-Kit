// Package config loads server configuration from environment with sane defaults.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime settings for hifihub-server.
type Config struct {
	ListenAddr      string
	DataDir         string
	ArtifactsDir    string
	DBPath          string
	DistDir         string
	GPGHome         string
	RegistryBaseURL string

	HeartbeatInterval time.Duration
	OfflineAfter      time.Duration
	SnapshotInterval  time.Duration

	// SignatureMode: "enforce" rejects unsigned artifacts; "dev" allows them
	// (for local bring-up only). Production must use "enforce" (NFR-S2).
	SignatureMode string

	// PlayGate controls the FR-3.10 idle gate. When true, jobs wait for
	// is_playing=false before any service-affecting step.
	PlayGateEnabled bool
	IdleWaitTimeout time.Duration

	// ZATAddr is the Zephyr Audio Transport listen address terminals dial into.
	ZATAddr string
	// MountDir is the parent directory for SMB/CIFS media-library mount points.
	MountDir string
	// MountCredDir holds transient 0600 CIFS credential files (root-owned).
	MountCredDir string

	// Version convergence: the server self-upgrades from the official release
	// feed and polls terminals to its own version. SelfEnabled toggles the feed
	// check; AutoApply applies unattended (still play-gated); FleetEnabled drives
	// terminal convergence. Interval bounds the polling cadence.
	UpdateFeedURL      string
	UpdateSelfEnabled  bool
	UpdateAutoApply    bool
	UpdateFleetEnabled bool
	UpdateInterval     time.Duration
	UpdateMaxParallel  int
	UpdateMaxAttempts  int
	UpdateRingPercent  int
	UpdateStageDir     string
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	c := Config{
		ListenAddr:        env("HIFIHUB_LISTEN", "0.0.0.0:8080"),
		DataDir:           env("HIFIHUB_DATA_DIR", "./data"),
		GPGHome:           env("HIFIHUB_GPG_HOME", ""),
		RegistryBaseURL:   env("HIFIHUB_REGISTRY_URL", ""),
		SignatureMode:     env("HIFIHUB_SIGNATURE_MODE", "enforce"),
		HeartbeatInterval: envDuration("HIFIHUB_HEARTBEAT", 15*time.Second),
		SnapshotInterval:  envDuration("HIFIHUB_SNAPSHOT", 10*time.Minute),
		PlayGateEnabled:   envBool("HIFIHUB_PLAY_GATE", true),
		IdleWaitTimeout:   envDuration("HIFIHUB_IDLE_TIMEOUT", 120*time.Minute),
	}
	c.ArtifactsDir = env("HIFIHUB_ARTIFACTS_DIR", c.DataDir+"/artifacts")
	c.DBPath = env("HIFIHUB_DB_PATH", c.DataDir+"/hifihub.db")
	c.DistDir = env("HIFIHUB_DIST_DIR", "/var/lib/hifihub-server/dist")
	c.ZATAddr = env("HIFIHUB_ZAT_ADDR", "0.0.0.0:7800")
	c.MountDir = env("HIFIHUB_MOUNT_DIR", "/mnt/zephyr")
	c.MountCredDir = env("HIFIHUB_MOUNT_CRED_DIR", c.DataDir+"/cifs")
	c.OfflineAfter = 3 * c.HeartbeatInterval
	c.UpdateFeedURL = env("HIFIHUB_UPDATE_FEED", "")
	c.UpdateSelfEnabled = envBool("HIFIHUB_UPDATE_SELF", true)
	c.UpdateAutoApply = envBool("HIFIHUB_UPDATE_AUTO", true)
	c.UpdateFleetEnabled = envBool("HIFIHUB_UPDATE_FLEET", true)
	c.UpdateInterval = envDuration("HIFIHUB_UPDATE_INTERVAL", 10*time.Minute)
	c.UpdateMaxParallel = envInt("HIFIHUB_UPDATE_MAX_PARALLEL", 2)
	c.UpdateMaxAttempts = envInt("HIFIHUB_UPDATE_MAX_ATTEMPTS", 3)
	c.UpdateRingPercent = envInt("HIFIHUB_UPDATE_RING_PERCENT", 20)
	c.UpdateStageDir = env("HIFIHUB_UPDATE_STAGE_DIR", c.DataDir+"/update")
	return c
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}
