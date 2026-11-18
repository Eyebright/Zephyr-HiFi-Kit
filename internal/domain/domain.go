// Package domain defines the core entities shared across the server.
// Field naming follows requirement doc v1.4 section 7.1.
package domain

import "encoding/json"

// AppCategory classifies a distributable application (FR-3.1.2).
type AppCategory string

const (
	CategoryRenderer   AppCategory = "renderer"
	CategoryDependency AppCategory = "dependency"
	CategoryTool       AppCategory = "tool"
	CategoryPlatform   AppCategory = "platform"
)

// Lifecycle states for an app version (FR-3.1.6).
const (
	LifecycleAvailable  = "available"
	LifecycleDeprecated = "deprecated"
	LifecycleRevoked    = "revoked"
)

// Protocol identifiers for the four renderer families (FR-2).
const (
	ProtoRAAT    = "raat"
	ProtoNAA     = "naa"
	ProtoAirPlay = "airplay"
	ProtoUPnP    = "upnp"
)

// App is a catalog entry describing a distributable application (FR-3.1).
type App struct {
	AppID             string      `json:"app_id"`
	Name              string      `json:"name"`
	Category          AppCategory `json:"category"`
	Upstream          string      `json:"upstream"`
	License           string      `json:"license"`
	MaintainerState   string      `json:"maintainer_state"`
	SelfBuildRequired bool        `json:"self_build_required"`
	DefaultChannel    string      `json:"default_channel"`
	CreatedAt         int64       `json:"created_at"`
}

// PortSpec describes a listening port declared by an app version (FR-3.1.4).
type PortSpec struct {
	Name         string `json:"name"`
	Proto        string `json:"proto"` // tcp|udp
	Port         int    `json:"port"`
	Range        int    `json:"range,omitempty"`
	Configurable bool   `json:"configurable"`
}

// AppVersion is a concrete, architecture-specific artifact (FR-3.1.3 / FR-3.11.9).
type AppVersion struct {
	ID               string     `json:"id"`
	AppID            string     `json:"app_id"`
	Version          string     `json:"version"`
	Arch             string     `json:"arch"` // amd64|arm64|armhf|armv7hf|all
	Format           string     `json:"format"`
	AptPackage       string     `json:"apt_package"` // format=apt: distro package name
	ArtifactPath     string     `json:"artifact_path"`
	Size             int64      `json:"size"`
	SHA256           string     `json:"sha256"`
	Signature        string     `json:"signature"`
	Signed           bool       `json:"signed"`
	BuildFlags       []string   `json:"build_flags"`
	Capabilities     []string   `json:"capabilities"`
	DistroSupport    []string   `json:"distro_support"`
	GlibcMin         string     `json:"glibc_min"`
	Units            []string   `json:"units"`
	Ports            []PortSpec `json:"ports"`
	ConfigTemplateID string     `json:"config_template_id"`
	DepsApps         []string   `json:"deps_apps"`
	DepsPkgs         []string   `json:"deps_pkgs"`
	OccupiesDAC      bool       `json:"occupies_dac"`
	BuildCommit      string     `json:"build_commit"`
	AirPlay2Enabled  bool       `json:"airplay2_enabled"`
	QPlayVer         int        `json:"qplay_ver"`
	Patched          bool       `json:"patched"`
	Banned           bool       `json:"banned"`
	Lifecycle        string     `json:"lifecycle"`
	BuildTime        int64      `json:"build_time"`
	PublishedAt      int64      `json:"published_at"`
}

// BomApp references one application inside a BOM (FR-3.2.1).
type BomApp struct {
	AppID   string `json:"app_id"`
	Version string `json:"version,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// Bom is a declarative desired-state bundle (FR-3.2).
type Bom struct {
	BomID           string            `json:"bom_id"`
	Version         int               `json:"version"`
	Role            string            `json:"role"`
	Apps            []BomApp          `json:"apps"`
	DepClosure      []string          `json:"dep_closure"`
	ConfigTemplates map[string]string `json:"config_templates"`
	Variables       map[string]string `json:"variables"`
	UnitsEnabled    []string          `json:"units_enabled"`
	AuxActions      []string          `json:"aux_actions"`
	Status          string            `json:"status"` // draft|published
	CreatedAt       int64             `json:"created_at"`
	UpdatedAt       int64             `json:"updated_at"`
}

// BOM statuses.
const (
	BomDraft     = "draft"
	BomPublished = "published"
)

// Node is a registered terminal (FR-4).
type Node struct {
	NodeID          string   `json:"node_id"`
	Alias           string   `json:"alias"`
	Group           string   `json:"group"`
	Tags            []string `json:"tags"`
	Arch            string   `json:"arch"` // amd64|arm64|armhf|armv7hf|android
	Distro          string   `json:"distro"`
	Kernel          string   `json:"kernel"`
	Status          string   `json:"status"` // online|offline|unknown
	LastSeen        int64    `json:"last_seen"`
	BomID           string   `json:"bom_id"`
	BomVersion      int      `json:"bom_version"`
	AgentVersion    string   `json:"agent_version"`
	Playing         bool     `json:"playing"`
	ActiveProto     string   `json:"active_proto"`
	DacHolderPID    int      `json:"dac_holder_pid"`
	CertFingerprint string   `json:"cert_fingerprint"`
	RegisteredAt    int64    `json:"registered_at"`

	// Management + licensing + OS-update + console fields (FR-3/4/6).
	Note           string `json:"note"`
	IP             string `json:"ip"`
	Authorized     bool   `json:"authorized"`
	AuthorizedAt   int64  `json:"authorized_at"`
	PlayProtect    bool   `json:"play_protect"`
	AutoOSUpdate   bool   `json:"auto_os_update"`
	OSWindow       string `json:"os_window"`
	OfflineAfter   int    `json:"offline_after"`
	DacArbitration string `json:"dac_arbitration"`
	DacDevice      string `json:"dac_device"`

	// Last reported resource usage, refreshed by every heartbeat (FR-4).
	CPU   float64 `json:"cpu"`
	MemMB float64 `json:"mem_mb"`
	TempC float64 `json:"temp_c"`

	// Secret authenticates the server->agent console proxy. It is never
	// serialized into node JSON; only the register response carries it once.
	Secret string `json:"-"`
}

// Node online states.
const (
	NodeOnline  = "online"
	NodeOffline = "offline"
	NodeUnknown = "unknown"
)

// InstalledApp is the per-node reported state of one application (FR-3.6).
type InstalledApp struct {
	NodeID     string   `json:"node_id"`
	AppID      string   `json:"app_id"`
	Version    string   `json:"version"`
	Package    string   `json:"package"`
	Unit       string   `json:"unit"`
	Enabled    bool     `json:"enabled"`
	ConfigHash string   `json:"config_hash"`
	BuildFlags []string `json:"build_flags"`
	Source     string   `json:"source"`
}

// Drift kinds (FR-3.6.1).
const (
	DriftMissing      = "missing"
	DriftExtra        = "extra"
	DriftVersionDiff  = "version_diff"
	DriftConfigDrift  = "config_drift"
	DriftUnitDisabled = "unit_disabled"
)

// Drift records an expected-vs-actual difference for one app on one node.
type Drift struct {
	NodeID  string `json:"node_id"`
	AppID   string `json:"app_id"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail"`
	FirstAt int64  `json:"first_at"`
}

// Job kinds.
const (
	KindProvision = "provision"
	KindUpgrade   = "upgrade"
	KindRollback  = "rollback"
	KindReconcile = "reconcile"
)

// Job states mirror the task state machine in doc 7.2.
const (
	JobQueued            = "queued"
	JobPrecheck          = "precheck"
	JobResolveDeps       = "resolve_deps"
	JobWaitingIdle       = "waiting_idle"
	JobDownloading       = "downloading"
	JobVerifying         = "verifying"
	JobInstalling        = "installing"
	JobRenderingConfig   = "rendering_config"
	JobEnablingUnits     = "enabling_units"
	JobValidating        = "validating"
	JobDone              = "done"
	JobRollback          = "rollback"
	JobRollbackVerify    = "rollback_verify"
	JobFailedRolledBack  = "failed_rolledback"
	JobFailedUnrecovered = "failed_unrecovered"
	JobSuspended         = "suspended"
	JobInterrupted       = "interrupted"
)

// Phase is one step of a job with its own state and timing (FR-3.5.1).
type Phase struct {
	Name       string `json:"name"`
	State      string `json:"state"` // pending|running|ok|failed|skipped
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Job is a provision/upgrade/rollback/reconcile task (FR-3.5 / 7.2).
type Job struct {
	ID         string  `json:"id"`
	NodeID     string  `json:"node_id"`
	BomID      string  `json:"bom_id"`
	Kind       string  `json:"kind"`
	State      string  `json:"state"`
	Phases     []Phase `json:"phases"`
	Reason     string  `json:"reason,omitempty"`
	CreatedAt  int64   `json:"created_at"`
	StartedAt  int64   `json:"started_at,omitempty"`
	FinishedAt int64   `json:"finished_at,omitempty"`
}

// Session is a unified cross-protocol playback session (FR-2.3.1).
// Metadata fields are best-effort: NAA exposes no session API.
type Session struct {
	ID           string `json:"id"`
	NodeID       string `json:"node_id"`
	Proto        string `json:"proto"`
	SourceClient string `json:"source_client,omitempty"`
	State        string `json:"state"`
	Format       string `json:"format,omitempty"`
	Volume       *int   `json:"volume,omitempty"`
	DacDevice    string `json:"dac_device,omitempty"`
	StartedAt    int64  `json:"started_at"`
	EndedAt      int64  `json:"ended_at,omitempty"`
}

// Alert severities (FR-7.1.1).
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Alert is a deduplicated, clustered notification (FR-7.1).
type Alert struct {
	ID         string `json:"id"`
	At         int64  `json:"at"`
	Kind       string `json:"kind"`
	Severity   string `json:"severity"`
	NodeID     string `json:"node_id,omitempty"`
	Message    string `json:"message"`
	Status     string `json:"status"` // open|ack|resolved
	AckAt      int64  `json:"ack_at"`
	ResolvedAt int64  `json:"resolved_at"`
}

// Alert statuses.
const (
	AlertOpen     = "open"
	AlertAck      = "ack"
	AlertResolved = "resolved"
)

// Audit is an append-only change record (FR-7.2).
type Audit struct {
	ID         string `json:"id"`
	Actor      string `json:"actor"`
	IP         string `json:"ip,omitempty"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail,omitempty"`
	Result     string `json:"result"` // ok|err
	DurationMS int64  `json:"duration_ms"`
	At         int64  `json:"at"`
}

// License is the single-row activation record (FR-6).
type License struct {
	Status         string `json:"status"` // none|activated|expired
	Key            string `json:"key,omitempty"`
	Account        string `json:"account,omitempty"`
	Seats          int    `json:"seats,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`
	ActivatedAt    int64  `json:"activated_at,omitempty"`
	BuiltinSeats   int    `json:"builtin_seats"`
	EffectiveSeats int    `json:"effective_seats"`
}

// License statuses.
const (
	LicenseNone      = "none"
	LicenseActivated = "activated"
	LicenseExpired   = "expired"
)

// Command types for the server->agent channel (FR-6).
const (
	CommandOSCheck      = "os-check"
	CommandOSUpgrade    = "os-upgrade"
	CommandNetdiag      = "netdiag"
	CommandAgentUpgrade = "agent-upgrade"
	CommandAptSource    = "apt-source"
	CommandAptInstall   = "apt-install"
)

// Command is a queued server->agent instruction and its eventual result.
type Command struct {
	ID           string          `json:"id"`
	NodeID       string          `json:"node_id"`
	Type         string          `json:"type"`
	Params       CommandParams   `json:"params"`
	Status       string          `json:"status"` // pending|done
	CreatedAt    int64           `json:"created_at"`
	FinishedAt   int64           `json:"finished_at,omitempty"`
	ResultStatus string          `json:"result_status,omitempty"`
	Log          string          `json:"log,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
}

// CommandParams carries optional command arguments.
type CommandParams struct {
	Packages []string `json:"packages,omitempty"`
	// agent-upgrade: the exact signed-chain artifact the terminal must install.
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	URL     string `json:"url,omitempty"`
	// apt-source: the repository entries the terminal must configure.
	Sources []AptSourceSpec `json:"sources,omitempty"`
}

// AptSource is a managed third-party APT repository. Distros lists the Debian
// codenames it applies to; empty means every apt-based terminal.
type AptSource struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Components string   `json:"components"`
	KeyURL     string   `json:"key_url"`
	Distros    []string `json:"distros"`
	CreatedAt  int64    `json:"created_at"`
}

// AptSourceSpec is the per-terminal wire form of an AptSource. An empty Suite
// lets the agent resolve its own codename from /etc/os-release.
type AptSourceSpec struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Suite      string `json:"suite,omitempty"`
	Components string `json:"components"`
	KeyURL     string `json:"key_url"`
}

// ServerUpdateState is the persisted self-update status of the management
// server: what it runs, what the feed offers, and where the apply pipeline is.
type ServerUpdateState struct {
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Available  bool   `json:"available"`
	State      string `json:"state"` // idle|checking|staged|applying|restarting|failed|offline
	Detail     string `json:"detail,omitempty"`
	CheckedAt  int64  `json:"checked_at,omitempty"`
	StagedAt   int64  `json:"staged_at,omitempty"`
	AutoApply  bool   `json:"auto_apply"`
	FeedURL    string `json:"feed_url,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	RestartReq bool   `json:"restart_required,omitempty"`
}

// FleetUpdatePolicy controls server-driven terminal convergence.
type FleetUpdatePolicy struct {
	Enabled     bool `json:"enabled"`
	MaxParallel int  `json:"max_parallel"`
	MaxAttempts int  `json:"max_attempts"`
	RingPercent int  `json:"ring_percent"` // canary ring size as % of fleet
}

// NodeUpdateState tracks one terminal's convergence toward the server version.
type NodeUpdateState struct {
	NodeID      string `json:"node_id"`
	Current     string `json:"current"`
	Target      string `json:"target"`
	State       string `json:"state"` // converged|pending|upgrading|waiting_idle|suspended|unsupported
	Attempts    int    `json:"attempts"`
	LastAttempt int64  `json:"last_attempt,omitempty"`
	CommandID   string `json:"command_id,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// OSPackage is one upgradable OS package reported by the agent.
type OSPackage struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
}

// OSUpdateState is the stored OS-update status for a node.
type OSUpdateState struct {
	LastCheck      int64       `json:"last_check"`
	RebootRequired bool        `json:"reboot_required"`
	Packages       []OSPackage `json:"packages"`
}

// NetdiagResult is one network diagnostic probe outcome.
type NetdiagResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	MS     int64  `json:"ms"`
}

// NetDiagState is the stored network-diagnostic status for a node.
type NetDiagState struct {
	At      int64           `json:"at"`
	Results []NetdiagResult `json:"results"`
}

// Rollout statuses.
const (
	RolloutPending = "pending"
	RolloutRunning = "running"
	RolloutDone    = "done"
)

// RolloutRing is one canary ring of a staged rollout (FR-3.5).
type RolloutRing struct {
	Name    string   `json:"name"`
	Percent int      `json:"percent"`
	Nodes   []string `json:"nodes"`
	Status  string   `json:"status"` // pending|running|done
	Jobs    []string `json:"jobs,omitempty"`
}

// Rollout is a staged, ring-based distribution plan.
type Rollout struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	BomID       string        `json:"bom_id"`
	BomVersion  int           `json:"bom_version"`
	Status      string        `json:"status"`
	CurrentRing int           `json:"current_ring"`
	CreatedAt   int64         `json:"created_at"`
	Rings       []RolloutRing `json:"rings"`
}

// Key is a GPG public key in the server keyring (FR-6 / NFR-S2).
type Key struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Algo        string `json:"algo"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   int64  `json:"created_at"`
	IsTrustRoot bool   `json:"is_trust_root"`
}

// BuildRecord captures a pipeline run for traceability (FR-3.11.10).
type BuildRecord struct {
	ID          string   `json:"id"`
	AppID       string   `json:"app_id"`
	Version     string   `json:"version"`
	Arch        string   `json:"arch"`
	Strategy    string   `json:"strategy"` // source|binary_repack
	SourceHash  string   `json:"source_hash"`
	PatchHashes []string `json:"patch_hashes"`
	BuildFlags  []string `json:"build_flags"`
	DepManifest string   `json:"dep_manifest"`
	BuildHost   string   `json:"build_host"`
	OutputHash  string   `json:"output_hash"`
	State       string   `json:"state"` // queued|building|ok|failed
	LogPath     string   `json:"log_path"`
	StartedAt   int64    `json:"started_at"`
	FinishedAt  int64    `json:"finished_at,omitempty"`
}

// ConfigTemplate holds a renderable configuration template (FR-3.4).
type ConfigTemplate struct {
	ID        string `json:"id"`
	AppID     string `json:"app_id"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	UpdatedAt int64  `json:"updated_at"`
}
