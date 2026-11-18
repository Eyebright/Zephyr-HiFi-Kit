package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/agent/internal/serverclient"
)

type aptRunner struct {
	calls [][]string
	fn    func(name string, args []string) (string, error)
}

func (r *aptRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.fn != nil {
		return r.fn(name, args)
	}
	return "ok", nil
}

func (r *aptRunner) ran(name string, args ...string) bool {
	for _, c := range r.calls {
		if c[0] != name || len(c) < len(args)+1 {
			continue
		}
		match := true
		for i, a := range args {
			if c[i+1] != a {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

func newAptExec(t *testing.T, run *aptRunner, playing bool) (*Executor, string) {
	t.Helper()
	e := New(run, "http://server", func() bool { return playing }, func() bool { return true })
	e.lookPath = func(string) (string, error) { return "/usr/bin/tool", nil }
	dir := t.TempDir()
	e.SetAptPaths(filepath.Join(dir, "keyrings"), filepath.Join(dir, "lists"), filepath.Join(dir, "os-release"))
	if err := os.WriteFile(filepath.Join(dir, "os-release"), []byte("VERSION_CODENAME=bookworm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e, dir
}

func keyServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAptSourceWritesListAndKey(t *testing.T) {
	key := []byte{0x99, 0x01, 0x02, 0x03}
	srv := keyServer(t, key)
	run := &aptRunner{}
	e, dir := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{
			ID: "upmpdcli", Name: "upmpdcli", URL: srv.URL + "/repo/", KeyURL: srv.URL + "/key.gpg", Components: "main",
		}},
	}})
	if res.Status != "ok" {
		t.Fatalf("apt-source: %s %s", res.Status, res.Log)
	}
	list, err := os.ReadFile(filepath.Join(dir, "lists", "zephyr-upmpdcli.list"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("deb [signed-by=%s] %s/repo/ bookworm main\n", filepath.Join(dir, "keyrings", "zephyr-upmpdcli.gpg"), srv.URL)
	if string(list) != want {
		t.Fatalf("list file:\n%s\nwant:\n%s", list, want)
	}
	got, err := os.ReadFile(filepath.Join(dir, "keyrings", "zephyr-upmpdcli.gpg"))
	if err != nil || string(got) != string(key) {
		t.Fatalf("keyring: %v %x", err, got)
	}
	if !run.ran("apt-get", "update", "-qq") {
		t.Fatalf("apt-get update not run: %v", run.calls)
	}
	data, ok := res.Data.(AptSourceData)
	if !ok || len(data.Sources) != 1 || data.Sources[0].Suite != "bookworm" {
		t.Fatalf("data: %+v", res.Data)
	}
}

func TestAptSourceArmoredKeyDearmored(t *testing.T) {
	srv := keyServer(t, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nabc\n-----END PGP PUBLIC KEY BLOCK-----\n"))
	run := &aptRunner{fn: func(name string, args []string) (string, error) {
		if name == "gpg" {
			// gpg --dearmor -o dest tmp
			return "", os.WriteFile(args[2], []byte{0x99}, 0o644)
		}
		return "ok", nil
	}}
	e, dir := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{ID: "arm", Name: "arm", URL: srv.URL + "/", KeyURL: srv.URL + "/key.asc"}},
	}})
	if res.Status != "ok" {
		t.Fatalf("armored: %s %s", res.Status, res.Log)
	}
	if _, err := os.Stat(filepath.Join(dir, "keyrings", "zephyr-arm.gpg")); err != nil {
		t.Fatal(err)
	}
	if !run.ran("gpg", "--dearmor") {
		t.Fatalf("gpg not run: %v", run.calls)
	}
}

func TestAptSourceCodenameFromPrettyName(t *testing.T) {
	run := &aptRunner{}
	e, dir := newAptExec(t, run, false)
	if err := os.WriteFile(filepath.Join(dir, "os-release"), []byte("PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{ID: "plain", Name: "plain", URL: "http://repo.example/"}},
	}})
	if res.Status != "ok" {
		t.Fatalf("pretty-name: %s %s", res.Status, res.Log)
	}
	list, _ := os.ReadFile(filepath.Join(dir, "lists", "zephyr-plain.list"))
	if !strings.Contains(string(list), " bookworm main") || strings.Contains(string(list), "signed-by") {
		t.Fatalf("list without key must omit signed-by: %s", list)
	}
}

func TestAptSourceRejectsBadIDAndSuite(t *testing.T) {
	run := &aptRunner{}
	e, dir := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{ID: "../evil", Name: "x", URL: "http://r/"}},
	}})
	if res.Status != "err" || len(run.calls) != 0 {
		t.Fatalf("bad id: %s %v", res.Status, run.calls)
	}
	if err := os.WriteFile(filepath.Join(dir, "os-release"), []byte("PRETTY_NAME=\"Moode Audio Player\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{ID: "ok", Name: "x", URL: "http://r/"}},
	}})
	if res.Status != "err" || !strings.Contains(res.Log, "代号") {
		t.Fatalf("unknown codename: %s %s", res.Status, res.Log)
	}
}

func TestAptSourceUpdateFailureKeepsFiles(t *testing.T) {
	run := &aptRunner{fn: func(name string, args []string) (string, error) {
		if name == "apt-get" {
			return "E: some error", fmt.Errorf("exit 100")
		}
		return "ok", nil
	}}
	e, dir := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-source", Params: serverclient.CommandParams{
		Sources: []serverclient.AptSourceSpec{{ID: "ok", Name: "x", URL: "http://r/"}},
	}})
	if res.Status != "err" {
		t.Fatalf("update failure must err: %s", res.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "lists", "zephyr-ok.list")); err != nil {
		t.Fatal(err)
	}
}

func TestAptInstallReportsVersions(t *testing.T) {
	run := &aptRunner{fn: func(name string, _ []string) (string, error) {
		if name == "dpkg-query" {
			return "mpd\t0.23.5-1\nupmpdcli\t1.8.9-1\n", nil
		}
		return "ok", nil
	}}
	e, _ := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-install", Params: serverclient.CommandParams{
		Packages: []string{"mpd", "upmpdcli"},
	}})
	if res.Status != "ok" {
		t.Fatalf("apt-install: %s %s", res.Status, res.Log)
	}
	data, ok := res.Data.(AptInstallData)
	if !ok || len(data.Installed) != 2 || data.Installed[0].Version != "0.23.5-1" {
		t.Fatalf("data: %+v", res.Data)
	}
	if !run.ran("apt-get", "install", "-y", "mpd", "upmpdcli") {
		t.Fatalf("install args: %v", run.calls)
	}
}

func TestAptInstallRepairsDepsOnce(t *testing.T) {
	installCalls := 0
	run := &aptRunner{fn: func(name string, args []string) (string, error) {
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			installCalls++
			if installCalls == 1 {
				return "unmet deps", fmt.Errorf("exit 100")
			}
		}
		if name == "dpkg-query" {
			return "mpd\t0.23.5-1\n", nil
		}
		return "ok", nil
	}}
	e, _ := newAptExec(t, run, false)
	res := e.Execute(context.Background(), serverclient.Command{Type: "apt-install", Params: serverclient.CommandParams{
		Packages: []string{"mpd"},
	}})
	if res.Status != "ok" || installCalls != 2 {
		t.Fatalf("repair retry: %s calls=%d %s", res.Status, installCalls, res.Log)
	}
	if !run.ran("apt-get", "-f", "install", "-y") {
		t.Fatalf("no -f repair: %v", run.calls)
	}
}

func TestAptInstallRejectsInjectionAndGate(t *testing.T) {
	run := &aptRunner{}
	e, _ := newAptExec(t, run, false)
	for _, pkg := range []string{"mpd; rm -rf /", "-o", "mpd\nx"} {
		res := e.Execute(context.Background(), serverclient.Command{Type: "apt-install", Params: serverclient.CommandParams{
			Packages: []string{pkg},
		}})
		if res.Status != "err" || len(run.calls) != 0 {
			t.Fatalf("package %q accepted: %s %v", pkg, res.Status, run.calls)
		}
	}
	ep, _ := newAptExec(t, run, true)
	res := ep.Execute(context.Background(), serverclient.Command{Type: "apt-install", Params: serverclient.CommandParams{
		Packages: []string{"mpd"},
	}})
	if res.Status != "err" || !strings.Contains(res.Log, "FR-3.10") {
		t.Fatalf("play gate: %s %s", res.Status, res.Log)
	}
}