// Package model holds the terminal agent's shared types: the local installed-app
// inventory, per-app configuration, and the built-in service registry that maps
// each distributable app to its systemd units, config template and UI fields.
// The server catalog remains authoritative for versions and artifacts; this
// registry is the agent's knowledge of how to install and configure each app.
package model

// InstalledApp is the agent's local record of one installed application.
type InstalledApp struct {
	AppID      string            `json:"app_id"`
	Version    string            `json:"version"`
	Package    string            `json:"package"`
	Unit       string            `json:"unit"`
	Enabled    bool              `json:"enabled"`
	ConfigHash string            `json:"config_hash"`
	Source     string            `json:"source"`
	Config     map[string]string `json:"config,omitempty"`
}

// State is the persisted local state of the agent. Secret is the per-node
// shared secret issued at registration, persisted in state.json (0600) and
// required on local mutation routes. It is never logged or exposed via GET.
type State struct {
	NodeID     string                  `json:"node_id"`
	Alias      string                  `json:"alias"`
	Arch       string                  `json:"arch"`
	Registered bool                    `json:"registered"`
	Secret     string                  `json:"secret,omitempty"`
	Apps       map[string]InstalledApp `json:"apps"`
}

// ConfigField describes one simple configuration input rendered in the local UI.
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

// ServiceDef is the agent's built-in definition of a distributable service.
type ServiceDef struct {
	AppID          string        `json:"app_id"`
	Name           string        `json:"name"`
	Protocol       string        `json:"protocol,omitempty"` // raat|naa|airplay|upnp|spotify|snapcast|slimproto
	Category       string        `json:"category"`
	Units          []string      `json:"units"`
	OccupiesDAC    bool          `json:"occupies_dac"`
	Primary        bool          `json:"primary"` // shown as a top-level card
	HelperFor      string        `json:"helper_for,omitempty"`
	DepsPkgs       []string      `json:"deps_pkgs,omitempty"`
	ConfigTemplate string        `json:"-"`
	ConfigPath     string        `json:"-"`
	ConfigFields   []ConfigField `json:"config_fields,omitempty"`
	// UnitTemplate marks a tar.bz2-installed app: the archive carries no systemd
	// unit, so the agent renders this template (var: .InstallDir) itself.
	UnitTemplate string        `json:"-"`
	Description  string        `json:"description,omitempty"`
}
