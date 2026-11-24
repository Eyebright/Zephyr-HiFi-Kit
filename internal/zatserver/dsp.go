package zatserver

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hifihub/server/internal/dsp"
	"hifihub/zat"
)

// loadDeviceDSP reads one device's remembered EQ, defaulting to a disabled flat curve.
func (s *Server) loadDeviceDSP(deviceKey string) (dsp.Config, error) {
	cfg := dsp.DefaultConfig()
	var enabled int
	var bands, peq string
	err := s.db.QueryRow(`SELECT enabled,preset,preamp,bands,mode,peq FROM playback_device_dsp WHERE device_key=?`, deviceKey).
		Scan(&enabled, &cfg.Preset, &cfg.Preamp, &bands, &cfg.Mode, &peq)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return dsp.DefaultConfig(), err
	}
	cfg.Enabled = enabled != 0
	if json.Unmarshal([]byte(bands), &cfg.Bands) != nil {
		cfg.Bands = make([]float64, dsp.BandCount)
	}
	if json.Unmarshal([]byte(peq), &cfg.PEQ) != nil {
		cfg.PEQ = nil
	}
	return dsp.Normalize(cfg), nil
}

func (s *Server) saveDeviceDSP(deviceKey string, cfg dsp.Config) error {
	bands, err := json.Marshal(cfg.Bands)
	if err != nil {
		return err
	}
	peq, err := json.Marshal(cfg.PEQ)
	if err != nil {
		return err
	}
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	_, err = s.db.Exec(`INSERT INTO playback_device_dsp(device_key,enabled,preset,preamp,bands,mode,peq,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(device_key) DO UPDATE SET enabled=excluded.enabled,preset=excluded.preset,preamp=excluded.preamp,bands=excluded.bands,mode=excluded.mode,peq=excluded.peq,updated_at=excluded.updated_at`,
		deviceKey, enabled, cfg.Preset, cfg.Preamp, string(bands), cfg.Mode, string(peq), time.Now().Unix())
	return err
}

// DeviceDSP returns a device's remembered EQ plus whether a live bit-perfect
// session currently forces it to bypass.
func (s *Server) DeviceDSP(nodeID, deviceKey string) (dsp.Config, bool, error) {
	target, err := parseDeviceKey(nodeID, deviceKey)
	if err != nil {
		return dsp.Config{}, false, err
	}
	cfg, err := s.loadDeviceDSP(target.DeviceKey)
	if err != nil {
		return dsp.Config{}, false, err
	}
	bypass := false
	if _, ps, perr := s.activePlay(nodeID, target.DeviceKey); perr == nil && ps != nil {
		ps.mu.Lock()
		bypass = ps.mqa || ps.dop
		ps.mu.Unlock()
	}
	return cfg, bypass, nil
}

// SetDeviceDSP persists a device's EQ and hot-updates a live session on that device.
func (s *Server) SetDeviceDSP(nodeID, deviceKey string, cfg dsp.Config) error {
	target, err := parseDeviceKey(nodeID, deviceKey)
	if err != nil {
		return err
	}
	cfg = dsp.Normalize(cfg)
	if cfg.Preset != "custom" {
		if _, ok := dsp.PresetByID(cfg.Preset); !ok {
			return fmt.Errorf("未知 EQ 预设：%s", cfg.Preset)
		}
	}
	if err := s.saveDeviceDSP(target.DeviceKey, cfg); err != nil {
		return err
	}
	if _, ps, perr := s.activePlay(nodeID, target.DeviceKey); perr == nil && ps != nil {
		ps.mu.Lock()
		if ps.dsp != nil {
			ps.dsp.Reconfigure(cfg, ps.rate, ps.mqa || ps.dop)
		}
		ps.mu.Unlock()
	}
	return nil
}

// attachDSP builds the per-stream EQ engine and publishes it on the play state.
func (s *Server) attachDSP(ps *playState, channels int) *dsp.Engine {
	cfg, err := s.loadDeviceDSP(ps.deviceKey)
	if err != nil {
		cfg = dsp.DefaultConfig()
	}
	ps.mu.Lock()
	bypass := ps.mqa || ps.dop
	rate, format := ps.rate, ps.format
	ps.mu.Unlock()
	eng := dsp.NewEngine(cfg, rate, channels, format, bypass)
	ps.mu.Lock()
	ps.dsp = eng
	ps.mu.Unlock()
	return eng
}

// processAudioFrame applies the stream EQ to an AUDIO frame's PCM in place.
func processAudioFrame(ps *playState, f zat.Frame) {
	if f.Header.Type != zat.TypeAudio || len(f.Payload) < 12 {
		return
	}
	off := 12 + int(binary.BigEndian.Uint32(f.Payload[8:12]))
	if off > len(f.Payload) {
		return
	}
	ps.mu.Lock()
	eng := ps.dsp
	ps.mu.Unlock()
	if eng != nil {
		eng.Process(f.Payload[off:])
	}
}