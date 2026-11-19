package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/license"
	"hifihub/server/internal/node"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
)

type stubFeed struct {
	rel Release
	err error
}

func (f stubFeed) Latest(context.Context) (Release, error) { return f.rel, f.err }

type harness struct {
	sv    *Service
	st    *store.Store
	nodes *node.Service
	cmds  *commands.Service
}

func newHarness(t *testing.T, feed Feed, opt Options) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ver := signature.New("dev", "")
	nodes := node.New(st, 90*time.Second)
	cat := catalog.New(st)
	boms := bom.New(st, cat)
	lic := license.New(st, ver)
	cmds := commands.New(st)
	tasks := task.New(st, nodes, boms, cat, false, time.Minute)
	sv := New(st, nodes, cmds, tasks, lic, ver, feed, opt)
	return &harness{sv: sv, st: st, nodes: nodes, cmds: cmds}
}

func (h *harness) insertNode(t *testing.T, id, arch, status, ver string, authorized, playing, protect bool) {
	t.Helper()
	_, err := h.st.DB.Exec(`INSERT INTO nodes
		(node_id,alias,arch,status,agent_version,authorized,playing,play_protect,registered_at,last_seen)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, id, arch, status, ver, b2i(authorized), b2i(playing), b2i(protect), time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (h *harness) setVer(t *testing.T, id, ver string) {
	t.Helper()
	if _, err := h.st.DB.Exec(`UPDATE nodes SET agent_version=? WHERE node_id=?`, ver, id); err != nil {
		t.Fatal(err)
	}
}

// writeDistSums populates a /dl/ dir with a SHA256SUMS covering the given debs.
func writeDistSums(t *testing.T, dir, ver string, archs []string) map[string]string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{}
	var lines string
	for _, a := range archs {
		deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", ver, a)
		content := []byte("DEB-" + ver + "-" + a)
		if err := os.WriteFile(filepath.Join(dir, deb), content, 0o644); err != nil {
			t.Fatal(err)
		}
		s := sha(content)
		sums[deb] = s
		lines += fmt.Sprintf("%s  %s\n", s, deb)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return sums
}

func TestCheckAvailable(t *testing.T) {
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.4.0"}}, Options{Current: "0.3.0", SelfEnabled: true})
	st, err := h.sv.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Available || st.Latest != "0.4.0" || st.State != "idle" {
		t.Fatalf("state=%+v", st)
	}
}

func TestCheckUpToDate(t *testing.T) {
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.3.0"}}, Options{Current: "0.3.0"})
	st, err := h.sv.Check(context.Background())
	if err != nil || st.Available {
		t.Fatalf("expected up-to-date: %+v err=%v", st, err)
	}
}

func TestCheckOffline(t *testing.T) {
	h := newHarness(t, stubFeed{err: errors.New("dial tcp: timeout")}, Options{Current: "0.3.0"})
	st, err := h.sv.Check(context.Background())
	if err == nil || st.State != "offline" || st.LastError == "" {
		t.Fatalf("expected offline: %+v err=%v", st, err)
	}
}

func TestCheckInvalidTag(t *testing.T) {
	h := newHarness(t, stubFeed{rel: Release{Tag: "v../../evil"}}, Options{Current: "0.3.0"})
	st, err := h.sv.Check(context.Background())
	if err == nil || st.State != "failed" {
		t.Fatalf("expected failed on illegal tag: %+v err=%v", st, err)
	}
}

func TestSetPolicyValidation(t *testing.T) {
	h := newHarness(t, stubFeed{}, Options{Current: "0.3.0"})
	if _, err := h.sv.SetPolicy(domain.FleetUpdatePolicy{Enabled: true, MaxParallel: 0, MaxAttempts: 3, RingPercent: 20}); err == nil {
		t.Fatal("expected max_parallel validation")
	}
	if _, err := h.sv.SetPolicy(domain.FleetUpdatePolicy{Enabled: true, MaxParallel: 2, MaxAttempts: 99, RingPercent: 20}); err == nil {
		t.Fatal("expected max_attempts validation")
	}
	if _, err := h.sv.SetPolicy(domain.FleetUpdatePolicy{Enabled: true, MaxParallel: 2, MaxAttempts: 3, RingPercent: 1}); err == nil {
		t.Fatal("expected ring_percent validation")
	}
	p, err := h.sv.SetPolicy(domain.FleetUpdatePolicy{Enabled: true, MaxParallel: 4, MaxAttempts: 5, RingPercent: 50})
	if err != nil || p.MaxParallel != 4 {
		t.Fatalf("valid policy rejected: %+v err=%v", p, err)
	}
	if h.sv.Policy().MaxParallel != 4 {
		t.Fatal("policy not persisted in memory")
	}
}

func TestNewRestoresRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prior := domain.ServerUpdateState{Current: "0.4.0", State: "restarting", RestartReq: true}
	if _, err := st.DB.Exec(`INSERT INTO server_state(key,value) VALUES('update.server',?)`, store.ToJSON(prior)); err != nil {
		t.Fatal(err)
	}
	ver := signature.New("dev", "")
	nodes := node.New(st, 90*time.Second)
	cat := catalog.New(st)
	lic := license.New(st, ver)
	tasks := task.New(st, nodes, bom.New(st, cat), cat, false, time.Minute)
	sv := New(st, nodes, commands.New(st), tasks, lic, ver, stubFeed{}, Options{Current: "0.4.0"})
	got := sv.ServerStatus()
	if got.State != "idle" || got.RestartReq || got.Detail == "" {
		t.Fatalf("restart not reconciled: %+v", got)
	}
}

// buildBundle creates a fake signed-chain release tarball and returns its bytes
// plus the dist-deb sha map. Structure mirrors scripts/build-release.sh output.
func buildBundle(t *testing.T, ver, arch string) ([]byte, map[string]string) {
	t.Helper()
	root := "Zephyr-HiFi-Kit-server-" + ver
	binName := "server/hifihub-server-linux-" + arch
	binContent := "NEWBINARY-" + ver
	debName := fmt.Sprintf("hifihub-agent_%s_%s.deb", ver, arch)
	debContent := "DEB-" + ver + "-" + arch
	installContent := "#!/bin/sh\necho install\n"
	distSums := fmt.Sprintf("%s  %s\n%s  agent-install.sh\n", sha([]byte(debContent)), debName, sha([]byte(installContent)))
	files := map[string]string{
		root + "/" + binName:                   binContent,
		root + "/server/dist/" + debName:       debContent,
		root + "/server/dist/agent-install.sh": installContent,
		root + "/server/dist/SHA256SUMS":       distSums,
	}
	rootSums := fmt.Sprintf("%s  %s\n%s  server/dist/%s\n%s  server/dist/agent-install.sh\n%s  server/dist/SHA256SUMS\n",
		sha([]byte(binContent)), binName,
		sha([]byte(debContent)), debName,
		sha([]byte(installContent)),
		sha([]byte(distSums)))
	files[root+"/SHA256SUMS"] = rootSums
	entries := []tarEntry{}
	for name, content := range files {
		entries = append(entries, reg(name, content))
	}
	return tarGz(t, entries), map[string]string{debName: sha([]byte(debContent))}
}

func TestApplyPipeline(t *testing.T) {
	ver, arch := "0.50", "amd64"
	tarBytes, debSums := buildBundle(t, ver, arch)
	tarName := "Zephyr-HiFi-Kit-server-" + ver + ".tar.gz"
	sum := sha(tarBytes)
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		assetURL := "http://" + r.Host + "/asset/" + tarName
		jsonFeedHandler("v"+ver, tarName, assetURL, "sha256:"+sum)(w, r)
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tarBytes) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmp := t.TempDir()
	exe := filepath.Join(tmp, "hifihub-server")
	if err := os.WriteFile(exe, []byte("OLDBINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	distDir := filepath.Join(tmp, "dist")
	restarted := false
	opt := Options{
		Current: "0.3.0", FeedURL: srv.URL + "/feed", SelfEnabled: true, GOARCH: arch,
		DistDir: distDir, StageDir: filepath.Join(tmp, "stage"), Executable: exe,
		Restart: func() error { restarted = true; return nil },
	}
	h := newHarness(t, NewFeed(opt.FeedURL), opt)
	st, err := h.sv.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v (state=%+v)", err, st)
	}
	if !restarted {
		t.Fatal("restart not invoked")
	}
	if st.State != "restarting" || !st.RestartReq {
		t.Fatalf("state=%+v", st)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEWBINARY-"+ver {
		t.Fatalf("binary not swapped: %q", b)
	}
	deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", ver, arch)
	if b, err := os.ReadFile(filepath.Join(distDir, deb)); err != nil || string(b) != "DEB-"+ver+"-"+arch {
		t.Fatalf("dist deb not refreshed: %v", err)
	}
	gotSums := readDistSums(distDir)
	if gotSums[deb] != debSums[deb] {
		t.Fatalf("dist sums mismatch: %q want %q", gotSums[deb], debSums[deb])
	}
}

func TestApplyBusyGate(t *testing.T) {
	ver, arch := "0.4.0", "amd64"
	tarBytes, _ := buildBundle(t, ver, arch)
	tarName := "Zephyr-HiFi-Kit-server-" + ver + ".tar.gz"
	sum := sha(tarBytes)
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		jsonFeedHandler("v"+ver, tarName, "http://"+r.Host+"/asset/"+tarName, "sha256:"+sum)(w, r)
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tarBytes) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tmp := t.TempDir()
	opt := Options{Current: "0.3.0", FeedURL: srv.URL + "/feed", GOARCH: arch,
		DistDir: filepath.Join(tmp, "dist"), StageDir: filepath.Join(tmp, "stage"),
		Executable: filepath.Join(tmp, "exe"), Restart: func() error { return nil }}
	h := newHarness(t, NewFeed(opt.FeedURL), opt)
	h.insertNode(t, "n-play", arch, domain.NodeOnline, "0.3.0", true, true, true)
	st, err := h.sv.Apply(context.Background())
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	if st.State != "waiting_idle" {
		t.Fatalf("state=%+v", st)
	}
	if b, _ := os.ReadFile(opt.Executable); string(b) == "NEWBINARY-"+ver {
		t.Fatal("binary swapped despite busy gate")
	}
}

func TestApplyUpToDate(t *testing.T) {
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.3.0"}}, Options{Current: "0.3.0"})
	if _, err := h.sv.Apply(context.Background()); err == nil {
		t.Fatal("expected up-to-date error")
	}
}

func fleetOpt(distDir string) Options {
	return Options{Current: "0.4.0", FleetEnabled: true, MaxParallel: 2, MaxAttempts: 3, RingPercent: 100, DistDir: distDir}
}

func TestFleetConvergence(t *testing.T) {
	distDir := t.TempDir()
	sums := writeDistSums(t, distDir, "0.4.0", []string{"amd64", "arm64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	h.insertNode(t, "n2", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	h.insertNode(t, "n3", "arm64", domain.NodeOnline, "0.3.0", true, false, true)
	h.insertNode(t, "n4", "android", domain.NodeOnline, "0.3.0", true, false, true)

	rep := h.sv.Converge(context.Background())
	if rep.Counts["upgrading"] != 2 || rep.Counts["pending"] != 1 || rep.Counts["unsupported"] != 1 {
		t.Fatalf("pass1 counts=%v", rep.Counts)
	}
	for _, id := range []string{"n1", "n2"} {
		cs, _ := h.cmds.List(id, "pending")
		if len(cs) != 1 || cs[0].Type != domain.CommandAgentUpgrade {
			t.Fatalf("%s command=%+v", id, cs)
		}
		deb := fmt.Sprintf("hifihub-agent_0.4.0_%s.deb", "amd64")
		if cs[0].Params.Version != "0.4.0" || cs[0].Params.SHA256 != sums[deb] || cs[0].Params.URL != "/dl/"+deb {
			t.Fatalf("%s params=%+v", id, cs[0].Params)
		}
		_, _ = h.cmds.Result(cs[0].ID, "ok", "installed", nil)
		h.setVer(t, id, "0.4.0")
	}
	rep = h.sv.Converge(context.Background())
	if rep.Counts["converged"] != 2 || rep.Counts["upgrading"] != 1 {
		t.Fatalf("pass2 counts=%v", rep.Counts)
	}
	cs, _ := h.cmds.List("n3", "pending")
	if len(cs) != 1 {
		t.Fatalf("n3 not issued: %+v", cs)
	}
	_, _ = h.cmds.Result(cs[0].ID, "ok", "installed", nil)
	h.setVer(t, "n3", "0.4.0")
	rep = h.sv.Converge(context.Background())
	if rep.Counts["converged"] != 3 || rep.Counts["unsupported"] != 1 {
		t.Fatalf("pass3 counts=%v", rep.Counts)
	}
	if rep.Ring != 1 {
		t.Fatalf("ring did not advance: %d", rep.Ring)
	}
}

func TestFleetGates(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "off", "amd64", domain.NodeOffline, "0.3.0", true, false, true)
	h.insertNode(t, "unauth", "amd64", domain.NodeOnline, "0.3.0", false, false, true)
	h.insertNode(t, "play", "amd64", domain.NodeOnline, "0.3.0", true, true, true)
	h.insertNode(t, "ok", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	rep := h.sv.Converge(context.Background())
	byID := map[string]domain.NodeUpdateState{}
	for _, r := range rep.Nodes {
		byID[r.NodeID] = r
	}
	if byID["off"].State != "offline" {
		t.Fatalf("off=%+v", byID["off"])
	}
	if byID["unauth"].State != "unsupported" {
		t.Fatalf("unauth=%+v", byID["unauth"])
	}
	if byID["play"].State != "waiting_idle" {
		t.Fatalf("play=%+v", byID["play"])
	}
	if cs, _ := h.cmds.List("play", "pending"); len(cs) != 0 {
		t.Fatal("command issued to playing node")
	}
	if byID["ok"].State != "upgrading" {
		t.Fatalf("ok=%+v", byID["ok"])
	}
}

func TestFleetMissingDist(t *testing.T) {
	h := newHarness(t, stubFeed{}, fleetOpt(t.TempDir()))
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	rep := h.sv.Converge(context.Background())
	if rep.Counts["unsupported"] != 1 {
		t.Fatalf("expected unsupported when /dl/ lacks deb: %v", rep.Counts)
	}
}

func TestFleetSuspendAndRetry(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	h.sv.Converge(context.Background())
	cs, _ := h.cmds.List("n1", "pending")
	if len(cs) != 1 {
		t.Fatalf("no command: %+v", cs)
	}
	_, _ = h.cmds.Result(cs[0].ID, "err", "dpkg failed", nil)
	if _, err := h.st.DB.Exec(`UPDATE update_nodes SET attempts=3,command_id='' WHERE node_id='n1'`); err != nil {
		t.Fatal(err)
	}
	rep := h.sv.Converge(context.Background())
	if rep.Counts["suspended"] != 1 {
		t.Fatalf("expected suspended: %v", rep.Counts)
	}
	if err := h.sv.RetryNode("n1"); err != nil {
		t.Fatal(err)
	}
	rep = h.sv.Converge(context.Background())
	if rep.Counts["upgrading"] != 1 {
		t.Fatalf("retry did not reissue: %v", rep.Counts)
	}
	if err := h.sv.RetryNode("ghost"); err == nil {
		t.Fatal("expected error retrying unknown node")
	}
}

func TestFleetInFlightNotSuspended(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	if _, err := h.sv.SetPolicy(domain.FleetUpdatePolicy{Enabled: true, MaxParallel: 2, MaxAttempts: 1, RingPercent: 100}); err != nil {
		t.Fatal(err)
	}
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	rep := h.sv.Converge(context.Background())
	if rep.Counts["upgrading"] != 1 {
		t.Fatalf("first pass should issue: %v", rep.Counts)
	}
	// attempts already equals the cap, but the command is still in flight:
	// it must stay upgrading, not be declared failed before it reports back.
	rep = h.sv.Converge(context.Background())
	if rep.Counts["suspended"] != 0 || rep.Counts["upgrading"] != 1 {
		t.Fatalf("in-flight command suspended prematurely: %v", rep.Counts)
	}
	cs, _ := h.cmds.List("n1", "pending")
	if len(cs) != 1 {
		t.Fatalf("command lost: %+v", cs)
	}
	_, _ = h.cmds.Result(cs[0].ID, "ok", "installed", nil)
	rep = h.sv.Converge(context.Background())
	if rep.Counts["upgrading"] != 1 {
		t.Fatalf("install ok should await heartbeat: %v", rep.Counts)
	}
	h.setVer(t, "n1", "0.4.0")
	rep = h.sv.Converge(context.Background())
	if rep.Counts["converged"] != 1 {
		t.Fatalf("heartbeat did not converge: %v", rep.Counts)
	}
}

func TestFleetDetailRefresh(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "n1", "amd64", domain.NodeOffline, "0.3.0", true, false, true)
	rep := h.sv.FleetStatus()
	if rep.Nodes[0].State != "offline" {
		t.Fatalf("expected offline: %+v", rep.Nodes[0])
	}
	if _, err := h.st.DB.Exec(`UPDATE nodes SET status='online',last_seen=? WHERE node_id='n1'`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	rep = h.sv.FleetStatus()
	r := rep.Nodes[0]
	if r.State != "pending" || strings.Contains(r.Detail, "离线") {
		t.Fatalf("stale offline detail after reconnect: %+v", r)
	}
}

func TestFleetCommandFailure(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	h.sv.Converge(context.Background())
	cs, _ := h.cmds.List("n1", "pending")
	if len(cs) != 1 {
		t.Fatalf("no command: %+v", cs)
	}
	_, _ = h.cmds.Result(cs[0].ID, "err", "dpkg: dependency problems", nil)
	// Automatic pass reconciles the failure but backoff holds reissuance.
	rep := h.sv.fleetTick(context.Background(), false, false)
	var row domain.NodeUpdateState
	for _, r := range rep.Nodes {
		if r.NodeID == "n1" {
			row = r
		}
	}
	if row.State != "pending" || row.CommandID != "" || row.Attempts != 1 {
		t.Fatalf("failure not reconciled: %+v", row)
	}
	if row.Detail == "" {
		t.Fatal("failure detail empty")
	}
	// Backoff holds on the automatic path: an immediate pass must not reissue.
	rep = h.sv.fleetTick(context.Background(), false, false)
	for _, r := range rep.Nodes {
		if r.NodeID == "n1" && r.State == "upgrading" {
			t.Fatal("reissued before backoff elapsed")
		}
	}
	// A forced converge pass bypasses backoff and reissues immediately.
	rep = h.sv.Converge(context.Background())
	if rep.Counts["upgrading"] != 1 {
		t.Fatalf("forced converge did not bypass backoff: %v", rep.Counts)
	}
}

func TestFleetStatusDryRun(t *testing.T) {
	distDir := t.TempDir()
	writeDistSums(t, distDir, "0.4.0", []string{"amd64"})
	h := newHarness(t, stubFeed{}, fleetOpt(distDir))
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.3.0", true, false, true)
	rep := h.sv.FleetStatus()
	if rep.Counts["pending"] != 1 {
		t.Fatalf("dry run should not issue: %v", rep.Counts)
	}
	if cs, _ := h.cmds.List("n1", "pending"); len(cs) != 0 {
		t.Fatal("dry run issued a command")
	}
}

func TestTickAutoApply(t *testing.T) {
	ver, arch := "0.4.0", "amd64"
	tarBytes, _ := buildBundle(t, ver, arch)
	tarName := "Zephyr-HiFi-Kit-server-" + ver + ".tar.gz"
	sum := sha(tarBytes)
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		jsonFeedHandler("v"+ver, tarName, "http://"+r.Host+"/asset/"+tarName, "sha256:"+sum)(w, r)
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tarBytes) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "exe")
	_ = os.WriteFile(exe, []byte("OLD"), 0o755)
	restarted := false
	opt := Options{Current: "0.3.0", FeedURL: srv.URL + "/feed", SelfEnabled: true, AutoApply: true,
		GOARCH: arch, DistDir: filepath.Join(tmp, "dist"), StageDir: filepath.Join(tmp, "stage"),
		Executable: exe, Restart: func() error { restarted = true; return nil }}
	h := newHarness(t, NewFeed(opt.FeedURL), opt)
	h.sv.Tick(context.Background())
	if !restarted {
		t.Fatal("auto-apply did not run on tick")
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEWBINARY-"+ver {
		t.Fatalf("binary not swapped by tick: %q", b)
	}
}
