package node

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Device is one registered playback output of a terminal. Name is the
// operator-assigned label; empty falls back to the agent-reported name.
type Device struct {
	HW           string `json:"hw"`
	Name         string `json:"name"`
	ReportedName string `json:"reported_name,omitempty"`
	Enabled      bool   `json:"enabled"`
	Sort         int    `json:"sort"`
	Volume       int    `json:"volume"`
	DSPEnabled   bool   `json:"dsp_enabled"`
	DSDMode      string `json:"dsd_mode"`
}

// Devices lists the registered outputs, seeding the terminal's current
// preferred device once so an upgrade never leaves playback without a choice.
func (s *Service) Devices(nodeID string) ([]Device, error) {
	n, err := s.Get(nodeID)
	if err != nil {
		return nil, err
	}
	seed := n.DacDevice
	if seed == "" {
		seed = "default"
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO node_devices(node_id,hw,name,enabled,sort) VALUES(?,?, '',1,0)`, nodeID, seed); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT d.hw,d.name,d.reported_name,d.enabled,d.sort,d.volume,COALESCE(e.enabled,0),d.dsd_mode
		FROM node_devices d LEFT JOIN playback_device_dsp e ON e.device_key = d.node_id || '::' || d.hw
		WHERE d.node_id=? ORDER BY d.sort, d.hw`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var enabled, dspEnabled int
		if err := rows.Scan(&d.HW, &d.Name, &d.ReportedName, &enabled, &d.Sort, &d.Volume, &dspEnabled, &d.DSDMode); err != nil {
			return nil, err
		}
		d.Enabled = enabled != 0
		d.DSPEnabled = dspEnabled != 0
		if d.DSDMode != "dop" {
			d.DSDMode = "pcm"
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SyncDevices registers every reported output without changing activation or
// operator labels, so hotplugged DACs become available in Terminal Center.
func (s *Service) SyncDevices(nodeID string, reported map[string]string) error {
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var nextSort int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort),-1)+1 FROM node_devices WHERE node_id=?`, nodeID).Scan(&nextSort); err != nil {
		return err
	}
	hws := make([]string, 0, len(reported))
	for hw := range reported {
		hws = append(hws, hw)
	}
	sort.Strings(hws)
	for _, hw := range hws {
		name := reported[hw]
		if hw == "" || len(hw) > 256 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO node_devices(node_id,hw,reported_name,sort) VALUES(?,?,?,?)
			ON CONFLICT(node_id,hw) DO UPDATE SET reported_name=CASE WHEN excluded.reported_name<>'' THEN excluded.reported_name ELSE node_devices.reported_name END`,
			nodeID, hw, name, nextSort); err != nil {
			return err
		}
		nextSort++
	}
	return tx.Commit()
}

// SetDeviceEnabled activates or deactivates one output for the player picker.
func (s *Service) SetDeviceEnabled(nodeID, hw string, enabled bool) error {
	if hw == "" {
		return fmt.Errorf("device hw required")
	}
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`INSERT INTO node_devices(node_id,hw,enabled) VALUES(?,?,?)
		ON CONFLICT(node_id,hw) DO UPDATE SET enabled=excluded.enabled`, nodeID, hw, v)
	return err
}

// SetDeviceDSDMode selects how DSD is delivered to one output: "pcm" converts
// server-side (safe for any DAC), "dop" passes DoP only for DoP-capable DACs.
func (s *Service) SetDeviceDSDMode(nodeID, hw, mode string) error {
	if hw == "" {
		return fmt.Errorf("device hw required")
	}
	if mode != "pcm" && mode != "dop" {
		return fmt.Errorf("invalid dsd mode %q", mode)
	}
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO node_devices(node_id,hw,dsd_mode) VALUES(?,?,?)
		ON CONFLICT(node_id,hw) DO UPDATE SET dsd_mode=excluded.dsd_mode`, nodeID, hw, mode)
	return err
}

// SetDeviceVolume stores one output's volume without changing other outputs.
func (s *Service) SetDeviceVolume(nodeID, hw string, volume int) error {
	if hw == "" {
		return fmt.Errorf("device hw required")
	}
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO node_devices(node_id,hw,volume) VALUES(?,?,?)
		ON CONFLICT(node_id,hw) DO UPDATE SET volume=excluded.volume`, nodeID, hw, volume)
	return err
}

// RenameDevice sets (or clears, with an empty name) the operator label.
func (s *Service) RenameDevice(nodeID, hw, name string) error {
	if hw == "" {
		return fmt.Errorf("device hw required")
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 40 {
		return fmt.Errorf("device name must be at most 40 characters")
	}
	for _, r := range name {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return fmt.Errorf("device name must not contain control characters")
		}
	}
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO node_devices(node_id,hw,name) VALUES(?,?,?)
		ON CONFLICT(node_id,hw) DO UPDATE SET name=excluded.name`, nodeID, hw, name)
	return err
}
