// Package ui serves the terminal's local web console (Zephyr Music Player
// style): browse to the terminal's IP to install/uninstall/enable/disable the
// audio services the server provides, watch performance, run system updates and
// network diagnostics, and reboot/shutdown. The UI depends only on the Backend
// interface so it can be tested with a fake. All asset and API paths are
// relative so the console also works behind the server reverse proxy at
// /api/v1/nodes/{id}/console/.
package ui

import (
	"context"

	"hifihub/agent/internal/audio"
	"hifihub/agent/internal/model"
	"hifihub/agent/internal/serverclient"
	"hifihub/agent/internal/sysinfo"
)

// Identity describes the terminal and its link to the server.
type Identity struct {
	NodeID          string `json:"node_id"`
	Alias           string `json:"alias"`
	Arch            string `json:"arch"`
	Registered      bool   `json:"registered"`
	ServerURL       string `json:"server_url"`
	ServerReachable bool   `json:"server_reachable"`
	SystemAvailable bool   `json:"system_available"`
	AllowPower      bool   `json:"allow_power"`
	AllowLocal      bool   `json:"allow_local"`
	AgentVersion    string `json:"agent_version"`
	Playing         bool   `json:"playing"`
	PlayProtect     bool   `json:"play_protect"`
	DacArbitration  string `json:"dac_arbitration"`
}

// GateState carries the cheap inputs for local mutation gates (no network).
// Secret is server-side only and is never rendered into GET responses.
type GateState struct {
	AllowLocal  bool
	Secret      string
	Playing     bool
	PlayProtect bool
}

// ServiceStatus is one service card's full state.
type ServiceStatus struct {
	AppID        string              `json:"app_id"`
	Name         string              `json:"name"`
	Protocol     string              `json:"protocol,omitempty"`
	Category     string              `json:"category"`
	Description  string              `json:"description,omitempty"`
	Primary      bool                `json:"primary"`
	Installed    bool                `json:"installed"`
	Version      string              `json:"version,omitempty"`
	Enabled      bool                `json:"enabled"`
	Running      bool                `json:"running"`
	ActiveState  string              `json:"active_state,omitempty"`
	SubState     string              `json:"sub_state,omitempty"`
	OccupiesDAC  bool                `json:"occupies_dac"`
	Helpers      []string            `json:"helpers,omitempty"`
	Config       map[string]string   `json:"config,omitempty"`
	ConfigFields []model.ConfigField `json:"config_fields,omitempty"`
}

// CatalogEntry is an installable app with its available versions.
type CatalogEntry struct {
	AppID    string   `json:"app_id"`
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

// Backend is everything the local UI needs from the agent core.
type Backend interface {
	Identity(ctx context.Context) Identity
	Services(ctx context.Context) []ServiceStatus
	Metrics() sysinfo.Metrics
	DAC() []audio.Holder
	Devices() []audio.ALSADevice
	Catalog(ctx context.Context) ([]CatalogEntry, error)
	Install(ctx context.Context, appID, version string, cfg map[string]string) error
	Uninstall(ctx context.Context, appID string) error
	Enable(appID string) error
	Disable(appID string) error
	Configure(appID string, cfg map[string]string) error
	Power(action string) error
	// GateState reports the local mutation gate inputs; Secret returns the
	// stored node secret (loopback-only endpoint). OSCheck/OSUpgrade/Netdiag run
	// terminal commands through the shared executor and return their results.
	GateState() GateState
	Secret() string
	OSCheck(ctx context.Context) serverclient.CommandResult
	OSUpgrade(ctx context.Context, packages []string) serverclient.CommandResult
	Netdiag(ctx context.Context) serverclient.CommandResult
}
