package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/audit"
	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/config"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/license"
	"hifihub/server/internal/node"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
	"hifihub/server/internal/update"
)

type offlineFeed struct{}

func (offlineFeed) Latest(context.Context) (update.Release, error) {
	return update.Release{}, errors.New("feed unreachable")
}

func setupUpdate(t *testing.T, feed update.Feed, opt update.Options) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ver := signature.New("dev", "")
	nodes := node.New(st, 90*time.Second)
	cat := catalog.New(st)
	lic := license.New(st, ver)
	cmds := commands.New(st)
	tasks := task.New(st, nodes, bom.New(st, cat), cat, false, time.Minute)
	if opt.DistDir == "" {
		opt.DistDir = filepath.Join(dir, "dist")
	}
	upd := update.New(st, nodes, cmds, tasks, lic, ver, feed, opt)
	a := New(config.Config{}, Deps{Nodes: nodes, Commands: cmds, Tasks: tasks, License: lic, Audit: audit.New(st), Update: upd})
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1", st
}

func insertUpdNode(t *testing.T, st *store.Store, id, arch, ver string) {
	t.Helper()
	_, err := st.DB.Exec(`INSERT INTO nodes
		(node_id,alias,arch,status,agent_version,authorized,playing,play_protect,registered_at,last_seen)
		VALUES(?,?,?,?,?,1,0,1,?,?)`, id, id, arch, domain.NodeOnline, ver, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func writeUpdDist(t *testing.T, dir, ver, arch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", ver, arch)
	if err := os.WriteFile(filepath.Join(dir, deb), []byte("deb"), 0o644); err != nil {
		t.Fatal(err)
	}
	sums := fmt.Sprintf("%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", deb)
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatusRoute(t *testing.T) {
	base, _ := setupUpdate(t, offlineFeed{}, update.Options{Current: "0.3.0"})
	s, b := doJSON(t, "GET", base+"/update/status", nil)
	if s != 200 {
		t.Fatalf("status: %d %s", s, b)
	}
	var out struct {
		Server domain.ServerUpdateState `json:"server"`
		Policy domain.FleetUpdatePolicy `json:"policy"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Server.Current != "0.3.0" {
		t.Fatalf("current=%q", out.Server.Current)
	}
}

func TestUpdateCheckOfflineRoute(t *testing.T) {
	base, _ := setupUpdate(t, offlineFeed{}, update.Options{Current: "0.3.0"})
	s, b := doJSON(t, "POST", base+"/update/check", map[string]any{})
	if s != 200 {
		t.Fatalf("check: %d %s", s, b)
	}
	var st domain.ServerUpdateState
	_ = json.Unmarshal(b, &st)
	if st.State != "offline" {
		t.Fatalf("state=%q", st.State)
	}
}

func TestUpdateAutoToggleRoute(t *testing.T) {
	base, _ := setupUpdate(t, offlineFeed{}, update.Options{Current: "0.3.0", AutoApply: true})
	s, b := doJSON(t, "POST", base+"/update/auto", map[string]any{"auto_apply": false})
	if s != 200 {
		t.Fatalf("auto: %d %s", s, b)
	}
	var st domain.ServerUpdateState
	_ = json.Unmarshal(b, &st)
	if st.AutoApply {
		t.Fatal("auto_apply not disabled")
	}
}

func TestUpdateApplyOfflineRoute(t *testing.T) {
	base, _ := setupUpdate(t, offlineFeed{}, update.Options{Current: "0.3.0"})
	s, _ := doJSON(t, "POST", base+"/update/apply", map[string]any{})
	if s != 400 {
		t.Fatalf("apply with unreachable feed should be 400, got %d", s)
	}
}

func TestUpdateFleetPolicyRoute(t *testing.T) {
	base, _ := setupUpdate(t, offlineFeed{}, update.Options{Current: "0.4.0"})
	s, b := doJSON(t, "POST", base+"/update/fleet/policy", map[string]any{
		"enabled": true, "max_parallel": 4, "max_attempts": 5, "ring_percent": 50,
	})
	if s != 200 {
		t.Fatalf("policy: %d %s", s, b)
	}
	var p domain.FleetUpdatePolicy
	_ = json.Unmarshal(b, &p)
	if p.MaxParallel != 4 || !p.Enabled {
		t.Fatalf("policy=%+v", p)
	}
	s, _ = doJSON(t, "POST", base+"/update/fleet/policy", map[string]any{
		"enabled": true, "max_parallel": 0, "max_attempts": 3, "ring_percent": 20,
	})
	if s != 400 {
		t.Fatalf("invalid policy should be 400, got %d", s)
	}
}

func TestUpdateFleetConvergeAndRetryRoute(t *testing.T) {
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	writeUpdDist(t, dist, "0.4.0", "amd64")
	base, st := setupUpdate(t, offlineFeed{}, update.Options{
		Current: "0.4.0", FleetEnabled: true, MaxParallel: 2, MaxAttempts: 3, RingPercent: 100, DistDir: dist,
	})
	insertUpdNode(t, st, "n1", "amd64", "0.3.0")
	s, b := doJSON(t, "POST", base+"/update/fleet/converge", map[string]any{})
	if s != 200 {
		t.Fatalf("converge: %d %s", s, b)
	}
	var rep update.FleetReport
	_ = json.Unmarshal(b, &rep)
	if rep.Counts["upgrading"] != 1 || rep.Target != "0.4.0" {
		t.Fatalf("report=%+v", rep)
	}
	s, b = doJSON(t, "GET", base+"/update/fleet", nil)
	if s != 200 {
		t.Fatalf("fleet: %d %s", s, b)
	}
	s, _ = doJSON(t, "POST", base+"/update/fleet/n1/retry", map[string]any{})
	if s != 200 {
		t.Fatalf("retry known node should be 200, got %d", s)
	}
	s, _ = doJSON(t, "POST", base+"/update/fleet/ghost/retry", map[string]any{})
	if s != 404 {
		t.Fatalf("retry unknown node should be 404, got %d", s)
	}
}
