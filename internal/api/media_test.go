package api

import (
	"runtime"
	"strings"
	"testing"
)

func TestMountCRUDAndRedaction(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "POST", base+"/mounts", map[string]any{
		"host": "nas.local", "share": "music", "username": "zephyr", "password": "s3cr3t",
	})
	if s != 201 {
		t.Fatalf("add mount: %d %s", s, b)
	}
	m := mustJSON(t, b)
	id := m["id"].(string)
	if m["status"] != "unmounted" {
		t.Fatalf("status: %v", m["status"])
	}
	if strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked: %s", b)
	}
	if runtime.GOOS == "windows" {
		if m["target"].(string) != "//nas.local/music" {
			t.Fatalf("target: %v", m["target"])
		}
	} else if !strings.HasPrefix(m["target"].(string), "/mnt/zephyr/") {
		t.Fatalf("target: %v", m["target"])
	}

	s, b = doJSON(t, "GET", base+"/mounts", nil)
	if s != 200 {
		t.Fatalf("list mounts: %d %s", s, b)
	}
	s, b = doJSON(t, "GET", base+"/mounts/"+id, nil)
	if s != 200 {
		t.Fatalf("get mount: %d %s", s, b)
	}
	if strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("password leaked on get: %s", b)
	}

	s, b = doJSON(t, "DELETE", base+"/mounts/"+id, nil)
	if s != 200 {
		t.Fatalf("delete mount: %d %s", s, b)
	}
	s, _ = doJSON(t, "GET", base+"/mounts/"+id, nil)
	if s != 404 {
		t.Fatalf("expected 404 after delete, got %d", s)
	}
}

func TestAddMountValidation(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "POST", base+"/mounts", map[string]any{"host": "a/b", "share": "music"})
	if s != 400 {
		t.Fatalf("expected 400 for invalid host, got %d %s", s, b)
	}
}

func TestTracksEmpty(t *testing.T) {
	base := setup(t, "dev")
	s, b := doJSON(t, "GET", base+"/tracks", nil)
	if s != 200 {
		t.Fatalf("list tracks: %d %s", s, b)
	}
	if strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("expected empty array, got %s", b)
	}
}

func TestPlayNotConnected(t *testing.T) {
	base := setup(t, "dev")
	nodeID := registerNode(t, base, "play-ep")
	s, b := doJSON(t, "POST", base+"/nodes/"+nodeID+"/play", map[string]any{"track_id": "trk-x"})
	if s != 409 {
		t.Fatalf("expected 409 not-connected, got %d %s", s, b)
	}
	s, b = doJSON(t, "POST", base+"/nodes/"+nodeID+"/stop", nil)
	if s != 409 {
		t.Fatalf("expected 409 stop not-connected, got %d %s", s, b)
	}
}

func TestPlayStatusDisconnected(t *testing.T) {
	base := setup(t, "dev")
	nodeID := registerNode(t, base, "status-ep")
	s, b := doJSON(t, "GET", base+"/nodes/"+nodeID+"/play", nil)
	if s != 200 {
		t.Fatalf("play status: %d %s", s, b)
	}
	st := mustJSON(t, b)
	if st["connected"] != false {
		t.Fatalf("expected connected=false, got %v", st["connected"])
	}
}

func TestPlayRequiresTrackID(t *testing.T) {
	base := setup(t, "dev")
	nodeID := registerNode(t, base, "notrack-ep")
	s, _ := doJSON(t, "POST", base+"/nodes/"+nodeID+"/play", map[string]any{})
	if s != 400 {
		t.Fatalf("expected 400 for missing track_id, got %d", s)
	}
}

func TestCoverNotFound(t *testing.T) {
	base := setup(t, "dev")
	s, _ := doJSON(t, "GET", base+"/covers/0000000000000000000000000000000000000000000000000000000000000000", nil)
	if s != 404 {
		t.Fatalf("expected 404 for unknown cover, got %d", s)
	}
}
