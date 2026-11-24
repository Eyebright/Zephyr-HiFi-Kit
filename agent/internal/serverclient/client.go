// Package serverclient is the terminal agent's HTTP client to the management
// server. The server is the sole source of applications (FR-3): the agent
// registers, reports state, pulls jobs and downloads artifacts from it. DTOs
// mirror the server's JSON contract.
package serverclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- DTOs (mirror server JSON) ---

// App is a catalog entry.
type App struct {
	AppID    string `json:"app_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// AppVersion is one architecture-specific artifact.
type AppVersion struct {
	ID              string   `json:"id"`
	AppID           string   `json:"app_id"`
	Version         string   `json:"version"`
	Arch            string   `json:"arch"`
	Format          string   `json:"format"`
	SHA256          string   `json:"sha256"`
	Size            int64    `json:"size"`
	Signed          bool     `json:"signed"`
	Units           []string `json:"units"`
	DepsPkgs        []string `json:"deps_pkgs"`
	OccupiesDAC     bool     `json:"occupies_dac"`
	AirPlay2Enabled bool     `json:"airplay2_enabled"`
	Capabilities    []string `json:"capabilities"`
	Banned          bool     `json:"banned"`
	Lifecycle       string   `json:"lifecycle"`
	PublishedAt     int64    `json:"published_at"`
}

// BomApp references an app inside a BOM.
type BomApp struct {
	AppID   string `json:"app_id"`
	Version string `json:"version"`
}

// Bom is a desired-state bundle.
type Bom struct {
	BomID           string            `json:"bom_id"`
	Version         int               `json:"version"`
	Role            string            `json:"role"`
	Apps            []BomApp          `json:"apps"`
	Variables       map[string]string `json:"variables"`
	ConfigTemplates map[string]string `json:"config_templates"`
	UnitsEnabled    []string          `json:"units_enabled"`
	Status          string            `json:"status"`
}

// Phase is one job step.
type Phase struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// Job is a provision/upgrade/rollback task.
type Job struct {
	ID     string  `json:"id"`
	NodeID string  `json:"node_id"`
	BomID  string  `json:"bom_id"`
	Kind   string  `json:"kind"`
	State  string  `json:"state"`
	Phases []Phase `json:"phases"`
	Reason string  `json:"reason"`
}

// Node is the registered terminal identity. Secret is the per-node shared
// secret issued at registration; the agent persists it (0600) and requires it
// on local mutation routes (X-Node-Secret). It is never logged.
type Node struct {
	NodeID string `json:"node_id"`
	Alias  string `json:"alias"`
	Arch   string `json:"arch"`
	Secret string `json:"secret"`
}

// Policy is the cached node policy returned by the heartbeat response. The
// agent uses PlayProtect and DacArbitration for local gates; the remaining
// fields are cached for reporting. OSWindow is kept raw so an unexpected shape
// (string or object) never breaks decoding of the gate-critical fields.
type Policy struct {
	OK             bool            `json:"ok"`
	PlayProtect    bool            `json:"play_protect"`
	AutoOSUpdate   bool            `json:"auto_os_update"`
	OSWindow       json.RawMessage `json:"os_window"`
	DacArbitration string          `json:"dac_arbitration"`
	DacDevice      string          `json:"dac_device"`
	OfflineAfter   int             `json:"offline_after"`
}

// QueueTrack is one server-side playlist entry with resolved library metadata.
type QueueTrack struct {
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	CoverHash  string `json:"cover_hash,omitempty"`
	Decodable  bool   `json:"decodable"`
	Missing    bool   `json:"missing"`
}

// QueueState is the node's server-side playback queue.
type QueueState struct {
	NodeID    string       `json:"node_id"`
	Tracks    []QueueTrack `json:"tracks"`
	Position  int          `json:"position"`
	Mode      string       `json:"mode,omitempty"`
	UpdatedAt int64        `json:"updated_at"`
}

// CommandParams carries optional command arguments. os-upgrade uses Packages;
// agent-upgrade uses the exact signed-chain artifact the terminal must install.
type CommandParams struct {
	Packages []string        `json:"packages,omitempty"`
	Version  string          `json:"version,omitempty"`
	Arch     string          `json:"arch,omitempty"`
	SHA256   string          `json:"sha256,omitempty"`
	URL      string          `json:"url,omitempty"`
	Sources  []AptSourceSpec `json:"sources,omitempty"`
}

// AptSourceSpec is one third-party APT repository to configure. An empty Suite
// means the agent resolves its own codename from /etc/os-release.
type AptSourceSpec struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Suite      string `json:"suite,omitempty"`
	Components string `json:"components"`
	KeyURL     string `json:"key_url"`
}

// Command is a server-issued terminal command
// (os-check|os-upgrade|netdiag|agent-upgrade).
type Command struct {
	ID        string        `json:"id"`
	NodeID    string        `json:"node_id"`
	Type      string        `json:"type"`
	Params    CommandParams `json:"params"`
	Status    string        `json:"status"`
	CreatedAt int64         `json:"created_at"`
}

// CommandResult is posted back to the server after a command runs. Data holds
// the typed payload (check data or netdiag probes); Log is the full text.
type CommandResult struct {
	Status string `json:"status"` // ok|err
	Log    string `json:"log"`
	Data   any    `json:"data"`
}

// Heartbeat is the periodic liveness report.
type Heartbeat struct {
	AgentVersion string  `json:"agent_version"`
	BuildFlavor  string  `json:"build_flavor,omitempty"`
	Playing      bool    `json:"playing"`
	ActiveProto  string  `json:"active_proto"`
	DacHolderPID int     `json:"dac_holder_pid"`
	CPU          float64 `json:"cpu"`
	MemMB        float64 `json:"mem_mb"`
	TempC        float64 `json:"temp_c"`
}

// InstalledApp is one reported inventory entry.
type InstalledApp struct {
	AppID      string   `json:"app_id"`
	Version    string   `json:"version"`
	Package    string   `json:"package"`
	Unit       string   `json:"unit"`
	Enabled    bool     `json:"enabled"`
	ConfigHash string   `json:"config_hash"`
	BuildFlags []string `json:"build_flags"`
	Source     string   `json:"source"`
}

// Snapshot is the full periodic state report.
type Snapshot struct {
	Alias  string         `json:"alias"`
	Arch   string         `json:"arch"`
	Distro string         `json:"distro"`
	Kernel string         `json:"kernel"`
	Apps   []InstalledApp `json:"apps"`
}

// Client talks to one management server.
type Client struct {
	base string
	http *http.Client
}

// New creates a client for base (e.g. http://host:8080).
func New(base string) *Client {
	return &Client{
		base: base + "/api/v1",
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server %s %s: %s", method, path, bytes.TrimSpace(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Health checks server reachability.
func (c *Client) Health(ctx context.Context) error {
	var m map[string]any
	return c.do(ctx, "GET", "/health", nil, &m)
}

// Register consumes a one-time token and returns the new node identity.
func (c *Client) Register(ctx context.Context, token, alias, arch, distro, kernel, machineID string) (Node, error) {
	var n Node
	body := map[string]string{
		"token": token, "alias": alias, "arch": arch,
		"distro": distro, "kernel": kernel, "machine_id": machineID,
	}
	err := c.do(ctx, "POST", "/nodes/register", body, &n)
	return n, err
}

// Heartbeat reports liveness and playback state and returns the node policy
// the server wants the agent to cache and enforce locally.
func (c *Client) Heartbeat(ctx context.Context, nodeID string, hb Heartbeat) (Policy, error) {
	var p Policy
	err := c.do(ctx, "POST", "/nodes/"+nodeID+"/heartbeat", hb, &p)
	return p, err
}

// ListCommands returns the pending terminal commands for a node.
func (c *Client) ListCommands(ctx context.Context, nodeID string) ([]Command, error) {
	var cs []Command
	err := c.do(ctx, "GET", "/nodes/"+nodeID+"/commands?status=pending", nil, &cs)
	return cs, err
}

// CommandResult posts the outcome (status, full log, typed data) of a command.
func (c *Client) CommandResult(ctx context.Context, id string, result CommandResult) error {
	return c.do(ctx, "POST", "/commands/"+id+"/result", result, nil)
}

// SendSnapshot reports the full inventory.
func (c *Client) SendSnapshot(ctx context.Context, nodeID string, snap Snapshot) error {
	return c.do(ctx, "POST", "/nodes/"+nodeID+"/snapshot", snap, nil)
}

// ListApps returns the catalog.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var apps []App
	err := c.do(ctx, "GET", "/apps", nil, &apps)
	return apps, err
}

// ListVersions returns all versions of an app.
func (c *Client) ListVersions(ctx context.Context, appID string) ([]AppVersion, error) {
	var vs []AppVersion
	err := c.do(ctx, "GET", "/apps/"+appID+"/versions", nil, &vs)
	return vs, err
}

// Version returns a specific app/version/arch artifact metadata.
func (c *Client) Version(ctx context.Context, appID, version, arch string) (AppVersion, error) {
	vs, err := c.ListVersions(ctx, appID)
	if err != nil {
		return AppVersion{}, err
	}
	for _, v := range vs {
		if v.Version == version && (arch == "" || v.Arch == arch) {
			return v, nil
		}
	}
	return AppVersion{}, fmt.Errorf("version %s %s (%s) not in catalog", appID, version, arch)
}

// Latest returns the newest available, non-banned version for an arch.
func (c *Client) Latest(ctx context.Context, appID, arch string) (AppVersion, error) {
	vs, err := c.ListVersions(ctx, appID)
	if err != nil {
		return AppVersion{}, err
	}
	var best *AppVersion
	for i := range vs {
		v := &vs[i]
		if v.Banned || v.Lifecycle == "revoked" {
			continue
		}
		if arch != "" && v.Arch != arch {
			continue
		}
		if best == nil || v.PublishedAt > best.PublishedAt || (v.PublishedAt == best.PublishedAt && v.Version > best.Version) {
			best = v
		}
	}
	if best == nil {
		return AppVersion{}, fmt.Errorf("no available version of %s for %s", appID, arch)
	}
	return *best, nil
}

// LatestBom returns the highest version of a BOM.
func (c *Client) LatestBom(ctx context.Context, bomID string) (Bom, error) {
	var b Bom
	err := c.do(ctx, "GET", "/boms/"+bomID, nil, &b)
	return b, err
}

// ListJobs returns jobs for a node.
func (c *Client) ListJobs(ctx context.Context, nodeID string) ([]Job, error) {
	var js []Job
	err := c.do(ctx, "GET", "/jobs?node_id="+nodeID, nil, &js)
	return js, err
}

// GetJob returns one job.
func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	var j Job
	err := c.do(ctx, "GET", "/jobs/"+id, nil, &j)
	return j, err
}

// AdvanceJob drives the server-side phases and play gate.
func (c *Client) AdvanceJob(ctx context.Context, id string) (Job, error) {
	var j Job
	err := c.do(ctx, "POST", "/jobs/"+id+"/advance", nil, &j)
	return j, err
}

// ReportPhase reports agent progress on a phase.
func (c *Client) ReportPhase(ctx context.Context, id, phase, state, detail string) (Job, error) {
	var j Job
	body := map[string]string{"phase": phase, "state": state, "detail": detail}
	err := c.do(ctx, "POST", "/jobs/"+id+"/phases", body, &j)
	return j, err
}

// Queue returns the node's server-side playback queue.
func (c *Client) Queue(ctx context.Context, nodeID string) (QueueState, error) {
	var q QueueState
	err := c.do(ctx, "GET", "/nodes/"+nodeID+"/queue", nil, &q)
	return q, err
}

// QueueNext advances the server queue and starts the next track.
func (c *Client) QueueNext(ctx context.Context, nodeID string) error {
	return c.do(ctx, "POST", "/nodes/"+nodeID+"/queue/next", nil, nil)
}

// QueuePrev goes back one queue entry (restarting the current one at the head).
func (c *Client) QueuePrev(ctx context.Context, nodeID string) error {
	return c.do(ctx, "POST", "/nodes/"+nodeID+"/queue/prev", nil, nil)
}

// QueueJump starts the queue entry at index.
func (c *Client) QueueJump(ctx context.Context, nodeID string, index int) error {
	return c.do(ctx, "POST", "/nodes/"+nodeID+"/queue/jump", map[string]int{"index": index}, nil)
}

// Download streams an artifact to dest and returns its SHA-256 hex digest.
func (c *Client) Download(ctx context.Context, appID, version, arch, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/repo/"+appID+"/"+version+"/"+arch, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s %s: status %d", appID, version, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DownloadDist fetches a server-root-relative distribution path (e.g.
// "/dl/hifihub-agent_0.4.0_amd64.deb") to dest and returns its sha256 hex. The
// path is always resolved against this agent's own management server, never an
// arbitrary host, so a command cannot redirect the download elsewhere.
func (c *Client) DownloadDist(ctx context.Context, path, dest string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return "", fmt.Errorf("invalid distribution path %q", path)
	}
	root := strings.TrimSuffix(c.base, "/api/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", root+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %d", path, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
