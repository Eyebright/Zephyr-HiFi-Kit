package zatserver

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidDeviceKey = errors.New("播放设备标识无效")

type playbackTarget struct {
	NodeID    string
	DeviceKey string
	HW        string
}

func makeDeviceKey(nodeID, hw string) string { return nodeID + "::" + hw }

func parseDeviceKey(nodeID, key string) (playbackTarget, error) {
	id, hw, ok := strings.Cut(key, "::")
	if !ok || id != nodeID || hw == "" || strings.ContainsAny(hw, "\x00\r\n") {
		return playbackTarget{}, ErrInvalidDeviceKey
	}
	return playbackTarget{NodeID: id, DeviceKey: key, HW: hw}, nil
}

func (s *Server) activeTarget(nodeID string) playbackTarget {
	n, err := s.nodes.Get(nodeID)
	if err != nil {
		return playbackTarget{NodeID: nodeID, DeviceKey: makeDeviceKey(nodeID, "default"), HW: "default"}
	}
	hw := n.DacDevice
	if hw == "" {
		hw = "default"
	}
	return playbackTarget{NodeID: nodeID, DeviceKey: makeDeviceKey(nodeID, hw), HW: hw}
}

func (s *Server) resolveTarget(nodeID, deviceKey string) (playbackTarget, error) {
	return s.resolveTargetEndpoint(nodeID, deviceKey, s.endpoint(nodeID))
}

func (s *Server) resolveConnectedTarget(nodeID, deviceKey string) (*endpoint, playbackTarget, error) {
	if deviceKey != "" {
		if _, err := parseDeviceKey(nodeID, deviceKey); err != nil {
			return nil, playbackTarget{}, err
		}
	}
	e := s.endpoint(nodeID)
	if e == nil {
		return nil, playbackTarget{}, ErrNotConnected
	}
	target, err := s.resolveTargetEndpoint(nodeID, deviceKey, e)
	if err != nil {
		return nil, playbackTarget{}, err
	}
	return e, target, nil
}

func (s *Server) resolveTargetEndpoint(nodeID, deviceKey string, e *endpoint) (playbackTarget, error) {
	explicit := deviceKey != ""
	if deviceKey == "" {
		if e != nil {
			e.mu.Lock()
			if e.play != nil {
				deviceKey = e.play.deviceKey
			}
			e.mu.Unlock()
		}
		if deviceKey == "" {
			deviceKey = s.activeTarget(nodeID).DeviceKey
		}
	}
	t, err := parseDeviceKey(nodeID, deviceKey)
	if err != nil {
		return playbackTarget{}, err
	}
	rows, err := s.nodes.Devices(nodeID)
	if err != nil {
		return playbackTarget{}, err
	}
	activated := false
	for _, d := range rows {
		if d.HW == t.HW && d.Enabled {
			activated = true
			break
		}
	}
	if !activated {
		return playbackTarget{}, fmt.Errorf("%w：%s", ErrDeviceNotActivated, t.HW)
	}
	if e == nil {
		if explicit {
			return playbackTarget{}, ErrNotConnected
		}
		return t, nil
	}
	e.mu.Lock()
	s.mu.Lock()
	current := s.conns[nodeID] == e
	s.mu.Unlock()
	if !current {
		e.mu.Unlock()
		return playbackTarget{}, ErrNotConnected
	}
	known := t.HW == "default"
	for _, hw := range e.caps.ALSADevices {
		if hw == t.HW {
			known = true
			break
		}
	}
	e.mu.Unlock()
	if !known {
		return playbackTarget{}, fmt.Errorf("%w：%s", ErrUnknownDevice, t.HW)
	}
	return t, nil
}

func (s *Server) targetNodeID(key string) (string, error) {
	id, _, ok := strings.Cut(key, "::")
	if !ok || id == "" {
		return "", ErrInvalidDeviceKey
	}
	if _, err := s.nodes.Get(id); err != nil {
		return "", err
	}
	return id, nil
}

// ActiveDevices lists activated outputs that are online and currently present.
func (s *Server) ActiveDevices() []PlayDevice {
	return s.activeDevices()
}

func (s *Server) activeDevices() []PlayDevice {
	nodes, err := s.nodes.List()
	if err != nil {
		return []PlayDevice{}
	}
	out := []PlayDevice{}
	for _, n := range nodes {
		if n.Status != "online" {
			continue
		}
		st, err := s.Status(n.NodeID)
		if err != nil || !st.Connected {
			continue
		}
		for _, d := range st.PlayDevices {
			if d.Present {
				out = append(out, d)
			}
		}
	}
	return out
}

// deviceDSDMode reports how DSD should be delivered to one output ("pcm" or
// "dop"); unknown devices default to the safe PCM conversion.
func (s *Server) deviceDSDMode(nodeID, hw string) string {
	rows, err := s.nodes.Devices(nodeID)
	if err == nil {
		for _, d := range rows {
			if d.HW == hw {
				return d.DSDMode
			}
		}
	}
	return "pcm"
}

func (s *Server) deviceVolume(nodeID, hw string) int {
	rows, err := s.nodes.Devices(nodeID)
	if err == nil {
		for _, d := range rows {
			if d.HW == hw {
				return d.Volume
			}
		}
	}
	return 100
}

func (s *Server) deviceName(nodeID, hw string) string {
	rows, err := s.nodes.Devices(nodeID)
	if err == nil {
		for _, d := range rows {
			if d.HW == hw && d.Name != "" {
				return d.Name
			}
		}
	}
	st, _ := s.Status(nodeID)
	for _, d := range st.DeviceInfo {
		if d.HW == hw && d.Name != "" {
			return d.Name
		}
	}
	return hw
}
