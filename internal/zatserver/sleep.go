package zatserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Sleep timers stop a playback device at a wall-clock deadline. They are
// persisted across restarts and remain attached to the device that was selected
// when set.

const (
	sleepTickMaxMinutes = 1440
	sleepCheckInterval  = 5 * time.Second
)

// ErrSleepMinutes is returned for out-of-range timer durations.
var ErrSleepMinutes = errors.New("定时时长需为 1..1440 分钟")

// loadSleepTimers restores persisted deadlines into memory at startup.
func (s *Server) loadSleepTimers() {
	rows, err := s.db.Query(`SELECT device_key,stop_at FROM playback_sleep_timer`)
	if err != nil {
		slog.Warn("zat load sleep timers", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var deviceKey string
		var stopAt int64
		if err := rows.Scan(&deviceKey, &stopAt); err != nil {
			slog.Warn("zat load sleep timers", "device_key", deviceKey, "err", err)
			return
		}
		s.sleepStop[deviceKey] = stopAt
	}
}

// SetSleepTimer arms (or overwrites) a node's active-device timer.
func (s *Server) SetSleepTimer(nodeID string, minutes int) (int64, error) {
	return s.setSleepTimer(s.activeTarget(nodeID), minutes)
}

func (s *Server) SetSleepTimerDevice(nodeID, deviceKey string, minutes int) (int64, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return 0, err
	}
	return s.setSleepTimer(target, minutes)
}

func (s *Server) setSleepTimer(target playbackTarget, minutes int) (int64, error) {
	if minutes < 1 || minutes > sleepTickMaxMinutes {
		return 0, ErrSleepMinutes
	}
	if _, err := s.nodes.Get(target.NodeID); err != nil {
		return 0, err
	}
	stopAt := time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	if _, err := s.db.Exec(`INSERT INTO playback_sleep_timer(device_key,stop_at) VALUES(?,?)
		ON CONFLICT(device_key) DO UPDATE SET stop_at=excluded.stop_at`, target.DeviceKey, stopAt); err != nil {
		return 0, fmt.Errorf("保存定时失败：%w", err)
	}
	s.sleepMu.Lock()
	s.sleepStop[target.DeviceKey] = stopAt
	s.sleepMu.Unlock()
	slog.Info("zat sleep timer armed", "node", target.NodeID, "device_key", target.DeviceKey,
		"minutes", minutes, "stop_at", stopAt)
	return stopAt, nil
}

// CancelSleepTimer removes a node's active-device timer; it is a no-op when none
// is set.
func (s *Server) CancelSleepTimer(nodeID string) error {
	return s.cancelSleepTimer(s.activeTarget(nodeID))
}

func (s *Server) CancelSleepTimerDevice(nodeID, deviceKey string) error {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return err
	}
	return s.cancelSleepTimer(target)
}

func (s *Server) cancelSleepTimer(target playbackTarget) error {
	if _, err := s.db.Exec(`DELETE FROM playback_sleep_timer WHERE device_key=?`, target.DeviceKey); err != nil {
		return fmt.Errorf("取消失败：%w", err)
	}
	s.sleepMu.Lock()
	delete(s.sleepStop, target.DeviceKey)
	s.sleepMu.Unlock()
	return nil
}

// SleepStopAt returns the node's active-device deadline, or 0 when no timer is
// set.
func (s *Server) SleepStopAt(nodeID string) int64 {
	target := s.activeTarget(nodeID)
	s.sleepMu.Lock()
	defer s.sleepMu.Unlock()
	return s.sleepStop[target.DeviceKey]
}

func (s *Server) SleepStopAtDevice(nodeID, deviceKey string) int64 {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		if deviceKey != "" && !errors.Is(err, ErrNotConnected) {
			return 0
		}
		target = s.activeTarget(nodeID)
		if deviceKey != "" {
			target, err = parseDeviceKey(nodeID, deviceKey)
			if err != nil {
				return 0
			}
			if _, err := s.targetNodeID(deviceKey); err != nil {
				return 0
			}
		}
	}
	s.sleepMu.Lock()
	defer s.sleepMu.Unlock()
	return s.sleepStop[target.DeviceKey]
}

func (s *Server) runSleepTimers(ctx context.Context) {
	t := time.NewTicker(sleepCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.fireSleepTimers(time.Now().Unix())
		}
	}
}

// fireSleepTimers stops playback for every deadline at or before now and
// clears those rows. A dead or disconnected terminal still loses its timer.
func (s *Server) fireSleepTimers(now int64) {
	s.sleepMu.Lock()
	due := []string{}
	for deviceKey, stopAt := range s.sleepStop {
		if stopAt > 0 && stopAt <= now {
			due = append(due, deviceKey)
			delete(s.sleepStop, deviceKey)
		}
	}
	s.sleepMu.Unlock()
	for _, deviceKey := range due {
		if _, err := s.db.Exec(`DELETE FROM playback_sleep_timer WHERE device_key=?`, deviceKey); err != nil {
			slog.Warn("zat sleep timer delete", "device_key", deviceKey, "err", err)
		}
		nodeID, err := s.targetNodeID(deviceKey)
		if err != nil {
			slog.Warn("zat sleep timer target", "device_key", deviceKey, "err", err)
			continue
		}
		if err := s.StopDevice(nodeID, deviceKey); err != nil && !errors.Is(err, ErrNotConnected) {
			slog.Warn("zat sleep timer stop", "node", nodeID, "device_key", deviceKey, "err", err)
			continue
		}
		slog.Info("zat sleep timer fired", "node", nodeID, "device_key", deviceKey)
	}
}
