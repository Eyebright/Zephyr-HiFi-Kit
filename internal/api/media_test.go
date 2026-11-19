package api

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"hifihub/server/internal/library"
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

	s, b = doJSON(t, "POST", base+"/mounts", map[string]any{"host": "NAS.LOCAL", "share": "music"})
	if s != http.StatusOK || mustJSON(t, b)["id"] != id || mustJSON(t, b)["reused"] != true {
		t.Fatalf("duplicate add must return 200 and the original ID: %d %s", s, b)
	}
	if strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked on reused add: %s", b)
	}
	s, b = doJSON(t, "POST", base+"/mounts/"+id+"/archive", nil)
	if s != http.StatusOK || mustJSON(t, b)["status"] != library.StatusArchived || mustJSON(t, b)["username"] != "" || mustJSON(t, b)["domain"] != "" {
		t.Fatalf("archive SMB source: %d %s", s, b)
	}
	if strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked on archive: %s", b)
	}
	s, b = doJSON(t, "POST", base+"/mounts", map[string]any{"host": "nas.local", "share": "music", "username": "replacement", "password": "synthetic-replacement"})
	if s != http.StatusOK || mustJSON(t, b)["id"] != id || mustJSON(t, b)["reused"] != true || mustJSON(t, b)["status"] != library.StatusUnmounted {
		t.Fatalf("re-add archived SMB source: %d %s", s, b)
	}
	if strings.Contains(string(b), "synthetic-replacement") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked on archive re-add: %s", b)
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

func TestLocalMountScanModesAndArchiveAdminBoundary(t *testing.T) {
	base := setup(t, "dev")
	root := t.TempDir()
	file := filepath.Join(root, "song.wav")
	// A real, tiny stereo PCM WAV avoids any decoder or network dependency.
	wav := make([]byte, 60)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 2)
	binary.LittleEndian.PutUint32(wav[24:28], 44100)
	binary.LittleEndian.PutUint32(wav[28:32], 44100*4)
	binary.LittleEndian.PutUint16(wav[32:34], 4)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], 16)
	if err := os.WriteFile(file, wav, 0o600); err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"kind": "local", "path": root}
	status, body := doJSON(t, "POST", base+"/mounts", req)
	if status != http.StatusCreated {
		t.Fatalf("add local source: %d %s", status, body)
	}
	id := mustJSON(t, body)["id"].(string)
	mountURL := base + "/mounts/" + id
	status, body = doJSON(t, "POST", mountURL+"/mount", nil)
	if status != http.StatusOK || mustJSON(t, body)["status"] != library.StatusMounted {
		t.Fatalf("mount local source: %d %s", status, body)
	}
	readMount := func() library.Mount {
		t.Helper()
		status, body := doJSON(t, "GET", mountURL, nil)
		var m library.Mount
		if status != http.StatusOK {
			t.Fatalf("get mount: %d %s", status, body)
		}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	readTracks := func() []library.Track {
		t.Helper()
		status, body := doJSON(t, "GET", base+"/tracks?mount_id="+id, nil)
		var tracks []library.Track
		if status != http.StatusOK {
			t.Fatalf("get tracks: %d %s", status, body)
		}
		if err := json.Unmarshal(body, &tracks); err != nil {
			t.Fatal(err)
		}
		return tracks
	}
	for _, test := range []struct {
		query string
		want  library.ScanResult
	}{
		{"", library.ScanResult{Count: 1, Added: 1}},
		{"?mode=update", library.ScanResult{Count: 1, Unchanged: 1}},
		{"?mode=full", library.ScanResult{Count: 1, Updated: 1}},
	} {
		status, body := doJSON(t, "POST", mountURL+"/scan"+test.query, nil)
		var result library.ScanResult
		if status != http.StatusOK {
			t.Fatalf("scan %q: %d %s", test.query, status, body)
		}
		if err := json.Unmarshal(body, &result); err != nil || result != test.want {
			t.Fatalf("scan %q result: %+v %v, want %+v", test.query, result, err, test.want)
		}
		for _, field := range []string{"count", "added", "updated", "unchanged", "removed"} {
			if _, ok := mustJSON(t, body)[field]; !ok {
				t.Fatalf("scan JSON missing %s: %s", field, body)
			}
		}
		if m := readMount(); m.LastScan == 0 || m.ScanResult != result {
			t.Fatalf("mount did not expose last successful result: %+v", m)
		}
	}
	beforeMount, beforeTracks := readMount(), readTracks()
	if len(beforeTracks) != 1 || beforeTracks[0].Codec != "pcm_s16le" {
		t.Fatalf("WAV fixture was not indexed: %+v", beforeTracks)
	}
	for _, mode := range []string{"incremental", "FULL", "unknown"} {
		status, body := doJSON(t, "POST", mountURL+"/scan?mode="+mode, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid mode %q: %d %s", mode, status, body)
		}
	}
	// Exercise actual route registration without setup's authenticated client.
	for _, action := range []string{"/archive", "/scan?mode=full", "/scan?mode=update"} {
		r, err := http.NewRequest("POST", mountURL+action, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", r.URL.Scheme+"://"+r.URL.Host)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unguarded %s: %d %s %v", action, resp.StatusCode, body, readErr)
		}
	}
	if readMount() != beforeMount || !reflect.DeepEqual(readTracks(), beforeTracks) {
		t.Fatal("invalid or unauthorized request changed catalog/statistics")
	}
	status, body = doJSON(t, "POST", mountURL+"/archive", nil)
	if status != http.StatusOK || mustJSON(t, body)["status"] != library.StatusArchived {
		t.Fatalf("authorized archive: %d %s", status, body)
	}
	archived := readMount()
	if archived.LastScan != beforeMount.LastScan || archived.ScanResult != beforeMount.ScanResult || !reflect.DeepEqual(readTracks(), beforeTracks) {
		t.Fatal("archive removed tracks or previous successful scan statistics")
	}
	status, body = doJSON(t, "POST", base+"/mounts", req)
	if status != http.StatusOK || mustJSON(t, body)["id"] != id || mustJSON(t, body)["reused"] != true || mustJSON(t, body)["status"] != library.StatusUnmounted {
		t.Fatalf("re-add archived local source: %d %s", status, body)
	}
	if !reflect.DeepEqual(readTracks(), beforeTracks) {
		t.Fatal("re-add replaced indexed tracks")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("archive removed source WAV: %v", err)
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
