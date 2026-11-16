package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/agent/internal/serverclient"
)

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return "fake-output", f.err
}

func (f *fakeRunner) ranDpkg(dest string) bool {
	for _, c := range f.calls {
		hasDpkg, hasI, hasDest := false, false, false
		for _, a := range c {
			switch a {
			case "dpkg":
				hasDpkg = true
			case "-i":
				hasI = true
			case dest:
				hasDest = true
			}
		}
		if hasDpkg && hasI && hasDest {
			return true
		}
	}
	return false
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func fakeDownload(content []byte, sum string, err error) DownloadFunc {
	return func(_ context.Context, _, dest string) (string, error) {
		if err != nil {
			return "", err
		}
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
			return "", mkErr
		}
		if wErr := os.WriteFile(dest, content, 0o644); wErr != nil {
			return "", wErr
		}
		return sum, nil
	}
}

func newUpgradeExec(t *testing.T, currentVer, arch string, dl DownloadFunc, run *fakeRunner, playing, protect bool) *Executor {
	t.Helper()
	e := New(run, "http://server", func() bool { return playing }, func() bool { return protect })
	e.SetUpgrade(arch, currentVer, t.TempDir(), dl)
	return e
}

func upParams() serverclient.CommandParams {
	return serverclient.CommandParams{
		Version: "0.4.0", Arch: "amd64",
		SHA256: sumOf([]byte("deb-bytes")), URL: "/dl/hifihub-agent_0.4.0_amd64.deb",
	}
}

func TestAgentUpgradeSuccess(t *testing.T) {
	content := []byte("deb-bytes")
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload(content, sumOf(content), nil), run, false, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "ok" {
		t.Fatalf("status=%s log=%s", res.Status, res.Log)
	}
	data, ok := res.Data.(UpgradeData)
	if !ok || !data.Installed || data.Version != "0.4.0" || data.SHA256 != sumOf(content) {
		t.Fatalf("data=%+v ok=%v", res.Data, ok)
	}
	dest := filepath.Join(e.dataDir, "updates", "hifihub-agent_0.4.0_amd64.deb")
	if !run.ranDpkg(dest) {
		t.Fatalf("dpkg -i not invoked: %v", run.calls)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("deb not staged: %v", err)
	}
}

func TestAgentUpgradeIdempotent(t *testing.T) {
	run := &fakeRunner{}
	called := false
	dl := func(_ context.Context, _, _ string) (string, error) { called = true; return "", nil }
	e := newUpgradeExec(t, "0.4.0", "amd64", dl, run, false, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "ok" {
		t.Fatalf("status=%s log=%s", res.Status, res.Log)
	}
	data := res.Data.(UpgradeData)
	if data.Installed {
		t.Fatal("should not reinstall when already at target version")
	}
	if called || len(run.calls) != 0 {
		t.Fatal("idempotent path must not download or run dpkg")
	}
}

func TestAgentUpgradePlayGate(t *testing.T) {
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload([]byte("x"), sumOf([]byte("deb-bytes")), nil), run, true, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "err" || !strings.Contains(res.Log, "FR-3.10") {
		t.Fatalf("play gate not enforced: %+v", res)
	}
	if len(run.calls) != 0 {
		t.Fatal("dpkg ran despite play gate")
	}
}

func TestAgentUpgradeArchGuards(t *testing.T) {
	run := &fakeRunner{}
	dl := fakeDownload([]byte("deb-bytes"), sumOf([]byte("deb-bytes")), nil)
	android := upParams()
	android.Arch = "android"
	e := newUpgradeExec(t, "0.3.0", "amd64", dl, run, false, true)
	if res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: android}); res.Status != "err" {
		t.Fatal("android arch should be rejected")
	}
	mismatch := upParams()
	mismatch.Arch = "arm64"
	if res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: mismatch}); res.Status != "err" {
		t.Fatal("arch mismatch should be rejected")
	}
}

func TestAgentUpgradeMissingParams(t *testing.T) {
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload(nil, "", nil), run, false, true)
	p := upParams()
	p.SHA256 = ""
	if res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: p}); res.Status != "err" {
		t.Fatal("missing sha256 should be rejected")
	}
}

func TestAgentUpgradeBadFilename(t *testing.T) {
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload(nil, "", nil), run, false, true)
	p := upParams()
	p.URL = "/dl/agent-install.sh"
	if res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: p}); res.Status != "err" {
		t.Fatal("non-deb filename should be rejected")
	}
}

func TestAgentUpgradeSHAMismatch(t *testing.T) {
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload([]byte("deb-bytes"), "deadbeef", nil), run, false, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "err" || !strings.Contains(res.Log, "SHA256") {
		t.Fatalf("sha mismatch not enforced: %+v", res)
	}
	if run.ranDpkg(filepath.Join(e.dataDir, "updates", "hifihub-agent_0.4.0_amd64.deb")) {
		t.Fatal("dpkg ran despite sha mismatch")
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "updates", "hifihub-agent_0.4.0_amd64.deb")); !os.IsNotExist(err) {
		t.Fatal("bad deb should be removed")
	}
}

func TestAgentUpgradeDownloadError(t *testing.T) {
	run := &fakeRunner{}
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload(nil, "", errors.New("connection refused")), run, false, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "err" || !strings.Contains(res.Log, "下载失败") {
		t.Fatalf("download error not surfaced: %+v", res)
	}
}

func TestAgentUpgradeDpkgError(t *testing.T) {
	run := &fakeRunner{err: errors.New("dpkg: dependency problems")}
	content := []byte("deb-bytes")
	e := newUpgradeExec(t, "0.3.0", "amd64", fakeDownload(content, sumOf(content), nil), run, false, true)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "err" || !strings.Contains(res.Log, "dpkg 安装失败") {
		t.Fatalf("dpkg error not surfaced: %+v", res)
	}
}

func TestAgentUpgradeNoDownload(t *testing.T) {
	run := &fakeRunner{}
	e := New(run, "http://server", func() bool { return false }, func() bool { return true })
	e.SetUpgrade("amd64", "0.3.0", t.TempDir(), nil)
	res := e.Execute(context.Background(), serverclient.Command{Type: "agent-upgrade", Params: upParams()})
	if res.Status != "err" {
		t.Fatal("missing download channel should be rejected")
	}
}
