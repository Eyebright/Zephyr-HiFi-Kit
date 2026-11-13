package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/alerts"
	"hifihub/server/internal/audit"
	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/config"
	"hifihub/server/internal/keys"
	"hifihub/server/internal/license"
	"hifihub/server/internal/metrics"
	"hifihub/server/internal/node"
	"hifihub/server/internal/repo"
	"hifihub/server/internal/rollouts"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
)

// setupFull builds a full API over a temporary SQLite store and returns the
// /api/v1 base URL, the server root URL (for /dl and /metrics) and the temp dir.
func setupFull(t *testing.T, sigMode string) (base, root, dir string) {
	t.Helper()
	dir = t.TempDir()
	cfg := config.Config{
		DataDir:         dir,
		ArtifactsDir:    filepath.Join(dir, "artifacts"),
		DistDir:         filepath.Join(dir, "dist"),
		DBPath:          filepath.Join(dir, "test.db"),
		SignatureMode:   sigMode,
		PlayGateEnabled: true,
		OfflineAfter:    45 * time.Second,
		IdleWaitTimeout: 120 * time.Minute,
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	verifier := signature.New(sigMode, "")
	cat := catalog.New(st)
	rp := repo.New(cfg.ArtifactsDir, verifier)
	boms := bom.New(st, cat)
	nodes := node.New(st, cfg.OfflineAfter)
	lic := license.New(st, verifier)
	ks := keys.New("", st)
	cmds := commands.New(st)
	aud := audit.New(st)
	alts := alerts.New(st, nodes, lic, cmds)
	tasks := task.New(st, nodes, boms, cat, lic, cfg.PlayGateEnabled, cfg.IdleWaitTimeout)
	rolls := rollouts.New(st, nodes, tasks, boms)
	met := metrics.New(cfg.DBPath)
	a := New(cfg, Deps{
		Cat: cat, Repo: rp, Boms: boms, Nodes: nodes, Tasks: tasks,
		License: lic, Keys: ks, Audit: aud, Alerts: alts, Commands: cmds,
		Rollouts: rolls, Metrics: met,
	})
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1", srv.URL, dir
}

// setup builds a full API and returns its /api/v1 base URL.
func setup(t *testing.T, sigMode string) string {
	t.Helper()
	base, _, _ := setupFull(t, sigMode)
	return base
}

// activateDevLicense activates a license in dev mode (signature verification is
// skipped), so job/authorization gates can be exercised without GPG.
func activateDevLicense(t *testing.T, base string, seats int) {
	t.Helper()
	payload := map[string]any{
		"key": "TEST-KEY-001", "account": "test@zephyr.hk",
		"seats": seats, "expires_at": time.Now().Add(24 * time.Hour).Unix(),
	}
	pj, _ := json.Marshal(payload)
	s, b := doJSON(t, "POST", base+"/license/activate", map[string]any{
		"payload_b64": base64.StdEncoding.EncodeToString(pj),
		"sig_b64":     base64.StdEncoding.EncodeToString([]byte("dev")),
	})
	if s != 200 {
		t.Fatalf("activate license: %d %s", s, b)
	}
}

// registerNode issues a token and registers a node, returning its node_id.
func registerNode(t *testing.T, base, alias string) string {
	t.Helper()
	s, b := doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800})
	if s != 201 {
		t.Fatalf("issue token: %d %s", s, b)
	}
	tok := mustJSON(t, b)["token"].(string)
	s, b = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": tok, "alias": alias, "arch": "arm64", "distro": "debian13", "machine_id": "mid-" + alias,
	})
	if s != 201 {
		t.Fatalf("register %s: %d %s", alias, s, b)
	}
	return mustJSON(t, b)["node_id"].(string)
}

func doJSON(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func upload(t *testing.T, base, app string, meta map[string]any, artifact, sig []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	mj, _ := json.Marshal(meta)
	_ = w.WriteField("meta", string(mj))
	fw, _ := w.CreateFormFile("artifact", "a.deb")
	fw.Write(artifact)
	if sig != nil {
		sw, _ := w.CreateFormFile("signature", "a.deb.asc")
		sw.Write(sig)
	}
	w.Close()
	resp, err := http.Post(base+"/apps/"+app+"/versions", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func mustJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad json: %s", b)
	}
	return m
}

// TestDistributionFlow exercises the full FR-3 distribution spine end to end.
func TestDistributionFlow(t *testing.T) {
	base := setup(t, "dev")

	// health
	s, b := doJSON(t, "GET", base+"/health", nil)
	if s != 200 {
		t.Fatalf("health: %d %s", s, b)
	}

	// catalog
	for _, app := range []string{"shairport-sync", "nqptp", "roonbridge"} {
		s, b := doJSON(t, "POST", base+"/apps", map[string]any{"app_id": app, "name": app, "category": "renderer"})
		if s != 201 {
			t.Fatalf("create app %s: %d %s", app, s, b)
		}
	}

	// upload shairport-sync (AirPlay2)
	shBytes := []byte("shairport-sync-airplay2-arm64-artifact")
	s, b = upload(t, base, "shairport-sync", map[string]any{
		"version": "4.3.2", "arch": "arm64", "format": "deb",
		"build_flags":      []string{"--with-airplay-2", "--with-alsa", "--with-soxr", "--with-avahi"},
		"capabilities":     []string{"AirPlay2"},
		"airplay2_enabled": true, "occupies_dac": true,
		"deps_pkgs": []string{"libconfig11", "libsoxr0", "avahi-daemon", "libasound2"},
		"units":     []string{"hifihub-shairport-sync.service"},
		"ports":     []map[string]any{{"name": "rtsp", "proto": "tcp", "port": 7000, "configurable": true}},
	}, shBytes, nil)
	if s != 201 {
		t.Fatalf("upload shairport: %d %s", s, b)
	}
	sh := mustJSON(t, b)
	wantHash := hex.EncodeToString(sha256Sum(shBytes))
	if sh["sha256"] != wantHash {
		t.Fatalf("sha256 mismatch: %v", sh["sha256"])
	}
	if ap := sh["artifact_path"].(string); !filepath.IsLocal(ap) || bytes.Contains([]byte(ap), []byte("\\")) {
		t.Fatalf("artifact_path not portable: %s", ap)
	}

	if s, b := upload(t, base, "nqptp", map[string]any{
		"version": "1.0", "arch": "arm64", "format": "deb", "occupies_dac": false,
		"units": []string{"hifihub-nqptp.service"},
		"ports": []map[string]any{{"name": "ptp", "proto": "udp", "port": 319}},
	}, []byte("nqptp-artifact"), nil); s != 201 {
		t.Fatalf("upload nqptp: %d %s", s, b)
	}
	if s, b := upload(t, base, "roonbridge", map[string]any{
		"version": "2.0", "arch": "arm64", "format": "deb", "occupies_dac": true,
		"capabilities": []string{"RAAT"}, "units": []string{"hifihub-roonbridge.service"},
	}, []byte("roonbridge-artifact"), nil); s != 201 {
		t.Fatalf("upload roonbridge: %d %s", s, b)
	}

	// BOM happy path
	s, b = doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": "living-airplay", "role": "airplay",
		"apps": []map[string]any{
			{"app_id": "shairport-sync", "version": "4.3.2"},
			{"app_id": "nqptp", "version": "1.0"},
		},
		"variables": map[string]string{"room": "living", "dac_device": "hw:1,0"},
	})
	if s != 201 {
		t.Fatalf("create bom: %d %s", s, b)
	}
	s, b = doJSON(t, "POST", base+"/boms/living-airplay/versions/1/validate?arch=arm64", nil)
	rep := mustJSON(t, b)
	if errs := rep["errors"].([]any); len(errs) != 0 {
		t.Fatalf("validate expected no errors: %s", b)
	}
	s, b = doJSON(t, "POST", base+"/boms/living-airplay/versions/1/publish?arch=arm64", nil)
	if s != 200 {
		t.Fatalf("publish: %d %s", s, b)
	}

	// NFR-R7: missing artifact blocks publish
	doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": "broken", "role": "airplay",
		"apps": []map[string]any{{"app_id": "shairport-sync", "version": "9.9.9"}},
	})
	if s, b := doJSON(t, "POST", base+"/boms/broken/versions/1/publish?arch=arm64", nil); s != 409 {
		t.Fatalf("NFR-R7 expected 409: %d %s", s, b)
	}

	// AC-38: capability/build mismatch
	upload(t, base, "shairport-sync", map[string]any{
		"version": "4.3.2-bad", "arch": "arm64", "format": "deb",
		"capabilities": []string{"AirPlay2"}, "airplay2_enabled": false, "occupies_dac": true,
	}, []byte("bad"), nil)
	doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": "ac38", "role": "airplay",
		"apps": []map[string]any{
			{"app_id": "shairport-sync", "version": "4.3.2-bad"},
			{"app_id": "nqptp", "version": "1.0"},
		},
	})
	s, b = doJSON(t, "POST", base+"/boms/ac38/versions/1/validate?arch=arm64", nil)
	if !bytes.Contains(b, []byte("AC-38")) {
		t.Fatalf("AC-38 expected error: %s", b)
	}

	// FR-3.8.4: DAC arbitration gate
	doJSON(t, "POST", base+"/boms", map[string]any{
		"bom_id": "full", "role": "full",
		"apps": []map[string]any{
			{"app_id": "shairport-sync", "version": "4.3.2"},
			{"app_id": "nqptp", "version": "1.0"},
			{"app_id": "roonbridge", "version": "2.0"},
		},
	})
	s, b = doJSON(t, "POST", base+"/boms/full/versions/1/validate?arch=arm64", nil)
	if !bytes.Contains(b, []byte("dac_arbitration")) {
		t.Fatalf("DAC arbitration expected error: %s", b)
	}

	// node registration
	s, b = doJSON(t, "POST", base+"/nodes/token", map[string]any{"ttl_seconds": 1800})
	tok := mustJSON(t, b)["token"].(string)
	s, b = doJSON(t, "POST", base+"/nodes/register", map[string]any{
		"token": tok, "alias": "living-pi", "arch": "arm64", "distro": "debian13", "machine_id": "mid-123",
	})
	if s != 201 {
		t.Fatalf("register: %d %s", s, b)
	}
	nid := mustJSON(t, b)["node_id"].(string)
	if s, _ := doJSON(t, "POST", base+"/nodes/register", map[string]any{"token": tok, "alias": "x", "arch": "arm64"}); s != 401 {
		t.Fatalf("token reuse expected 401: %d", s)
	}

	// License + authorization gate (FR-6): a job requires an activated license
	// AND an authorized node holding a seat.
	activateDevLicense(t, base, 10)
	if s, b := doJSON(t, "POST", base+"/jobs", map[string]any{"node_id": nid, "bom_id": "living-airplay", "kind": "provision"}); s != 409 {
		t.Fatalf("job before authorize expected 409, got %d %s", s, b)
	}
	if s, b := doJSON(t, "POST", base+"/nodes/"+nid+"/authorize", nil); s != 200 {
		t.Fatalf("authorize: %d %s", s, b)
	}

	// play gate: playing -> waiting_idle
	doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{"playing": true, "active_proto": "airplay"})
	s, b = doJSON(t, "POST", base+"/jobs", map[string]any{"node_id": nid, "bom_id": "living-airplay", "kind": "provision"})
	if s != 201 {
		t.Fatalf("create job: %d %s", s, b)
	}
	jid := mustJSON(t, b)["id"].(string)
	s, b = doJSON(t, "POST", base+"/jobs/"+jid+"/advance", nil)
	if st := mustJSON(t, b)["state"]; st != "waiting_idle" {
		t.Fatalf("play gate expected waiting_idle, got %v (%s)", st, b)
	}

	// idle -> downloading
	doJSON(t, "POST", base+"/nodes/"+nid+"/heartbeat", map[string]any{"playing": false})
	s, b = doJSON(t, "POST", base+"/jobs/"+jid+"/advance", nil)
	if st := mustJSON(t, b)["state"]; st != "downloading" {
		t.Fatalf("expected downloading, got %v (%s)", st, b)
	}

	// agent phases -> done
	for _, ph := range []string{"downloading", "verifying", "installing", "rendering_config", "enabling_units", "validating"} {
		if s, b := doJSON(t, "POST", base+"/jobs/"+jid+"/phases", map[string]any{"phase": ph, "state": "ok"}); s != 200 {
			t.Fatalf("phase %s: %d %s", ph, s, b)
		}
	}
	s, b = doJSON(t, "GET", base+"/jobs/"+jid, nil)
	if st := mustJSON(t, b)["state"]; st != "done" {
		t.Fatalf("expected done, got %v", st)
	}

	// download + Range + sha256 header
	resp, err := http.Get(base + "/repo/shairport-sync/4.3.2/arm64")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, shBytes) {
		t.Fatalf("download mismatch")
	}
	if resp.Header.Get("X-Artifact-Sha256") != wantHash {
		t.Fatalf("sha256 header mismatch")
	}
	req, _ := http.NewRequest("GET", base+"/repo/shairport-sync/4.3.2/arm64", nil)
	req.Header.Set("Range", "bytes=0-8")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	if r2.StatusCode != 206 || !bytes.Equal(part, shBytes[:9]) {
		t.Fatalf("range expected 206 + first 9 bytes, got %d %q", r2.StatusCode, part)
	}
}

// TestEnforceRejectsUnsigned verifies NFR-S2 in enforce mode.
func TestEnforceRejectsUnsigned(t *testing.T) {
	base := setup(t, "enforce")
	doJSON(t, "POST", base+"/apps", map[string]any{"app_id": "shairport-sync", "name": "x", "category": "renderer"})
	s, b := upload(t, base, "shairport-sync", map[string]any{
		"version": "4.3.2", "arch": "arm64", "format": "deb",
	}, []byte("unsigned-artifact"), nil)
	if s != 400 {
		t.Fatalf("enforce mode must reject unsigned artifact, got %d %s", s, b)
	}
	if !bytes.Contains(b, []byte("NFR-S2")) {
		t.Fatalf("expected NFR-S2 rejection message: %s", b)
	}
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

var _ = fmt.Sprintf
