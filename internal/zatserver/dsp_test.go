package zatserver

import (
	"testing"

	"hifihub/server/internal/dsp"
)

func TestDeviceDSPDefaultsAndPersistsPerDevice(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "dsp-node", true)
	key1 := nodeID + "::hw:1,0"
	key2 := nodeID + "::hw:2,0"

	cfg, bypass, err := h.srv.DeviceDSP(nodeID, key1)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || bypass || cfg.Preset != "flat" {
		t.Fatalf("default dsp = %+v bypass=%v", cfg, bypass)
	}

	bass, _ := dsp.PresetByID("bass")
	want := dsp.Config{Enabled: true, Preset: "bass", Preamp: bass.Preamp, Bands: bass.Bands}
	if err := h.srv.SetDeviceDSP(nodeID, key1, want); err != nil {
		t.Fatal(err)
	}
	got, _, err := h.srv.DeviceDSP(nodeID, key1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Preset != "bass" || got.Bands[0] != bass.Bands[0] {
		t.Fatalf("persisted dsp = %+v", got)
	}
	// The other device keeps its own (default) memory.
	other, _, err := h.srv.DeviceDSP(nodeID, key2)
	if err != nil {
		t.Fatal(err)
	}
	if other.Enabled || other.Preset != "flat" {
		t.Fatalf("second device leaked config: %+v", other)
	}
}

func TestSetDeviceDSPRejectsUnknownPresetAndBadKey(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "dsp-node", true)
	if err := h.srv.SetDeviceDSP(nodeID, nodeID+"::hw:1,0", dsp.Config{Enabled: true, Preset: "nope"}); err == nil {
		t.Fatal("unknown preset accepted")
	}
	if err := h.srv.SetDeviceDSP(nodeID, "wrong::hw:1,0", dsp.DefaultConfig()); err == nil {
		t.Fatal("mismatched device key accepted")
	}
}