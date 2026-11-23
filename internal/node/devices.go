package node

import (
	"fmt"
	"unicode/utf8"
)

// Device is one registered playback output of a terminal. Name is the
// operator-assigned label; empty falls back to the agent-reported name.
type Device struct {
	HW      string `json:"hw"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
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
	rows, err := s.db.Query(`SELECT hw,name,enabled,sort FROM node_devices WHERE node_id=? ORDER BY sort, hw`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var enabled int
		if err := rows.Scan(&d.HW, &d.Name, &enabled, &d.Sort); err != nil {
			return nil, err
		}
		d.Enabled = enabled != 0
		out = append(out, d)
	}
	return out, rows.Err()
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