//go:build linux

package androidcore

import (
	"encoding/json"
	"strings"
	"testing"
)

// The self test reads these three answers from Java, so their shape is an
// interface even though nothing here talks to a real DAC.
func TestUsbToneFacadeWithoutARun(t *testing.T) {
	var out toneAnswer
	if err := json.Unmarshal([]byte(UsbToneStat()), &out); err != nil {
		t.Fatalf("UsbToneStat is not JSON: %v", err)
	}
	if out.OK || !strings.Contains(out.Error, "no USB tone run") {
		t.Errorf("UsbToneStat before a run = %+v", out)
	}
	if err := json.Unmarshal([]byte(UsbToneStop()), &out); err != nil {
		t.Fatalf("UsbToneStop is not JSON: %v", err)
	}
	if out.OK {
		t.Errorf("UsbToneStop before a run claimed success: %+v", out)
	}
}

func TestUsbToneFacadeRejectsBadRequests(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal([]byte(UsbToneStart("{")), &out); err != nil {
		t.Fatalf("UsbToneStart with a broken body is not JSON: %v", err)
	}
	if err, _ := out["error"].(string); err == "" {
		t.Error("a malformed request reported no error")
	}
	// A descriptor the kernel will not accept must come back as an error, not a
	// panic and not a run that looks healthy with zero samples.
	body := `{"fd":3,"endpoint":1,"rate":44100,"channels":2,"subslot":2,"duration_ms":100}`
	if err := json.Unmarshal([]byte(UsbToneStart(body)), &out); err != nil {
		t.Fatalf("UsbToneStart with a stray descriptor is not JSON: %v", err)
	}
	if ok, _ := out["ok"].(bool); ok {
		t.Errorf("a descriptor no device is behind started a run: %v", out)
	}
	if err, _ := out["error"].(string); err == "" {
		t.Error("a stray descriptor reported no error")
	}
}
