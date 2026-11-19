package executor

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hifihub/agent/internal/serverclient"
)

// roonFixture is a minimal RoonBridge-shaped tar.bz2 (top dir RoonBridge/ with
// Bridge/RoonBridge, Bridge/VERSION and extra.conf), generated offline because
// the standard library only ships a bzip2 reader.
const roonFixture = "QlpoOTFBWSZTWesMnCQAAQP/gcOQEABoYf+AFqmbAH/lnkAAKIAIMAEYKhpQJgJgAmjEwAAammMBoDQAAaaDQDQBoEUkmag1NMmmCaA0epoeUMhtS/h3e2Yzmoio90isgBjEip17ZyiFdkog6SWuMaTepIESSctfRbTUnFJ0N0HHsvnmy3bF7Y5VNYmmASzhZb35xLBNshOnNRk3JCQkV0pBZBNMshMKU6JTTyJGJIxm+dGGsxk5Q4hKUok0iZCJxTKyqpIhMAlTCUpo3wEEYLougbYtyTwfHX/VTwcQrF5ClHRWmExiAxIDRSExxDfMgPyLZA587UrPtYTjIZ6x0pq9f5DPWlOVL8t6bNygOxdyRThQkOsMnCQ="

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(roonFixture)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTarExec(t *testing.T) (*Executor, *fakeSource, *fakeUnits, *fakePackages, *fakeInventory, string) {
	t.Helper()
	root := t.TempDir()
	bin := fixtureBytes(t)
	src := &fakeSource{
		ver: serverclient.AppVersion{
			AppID: "roonbridge", Version: "2.70.1671", Arch: "arm64",
			Format: "tar.bz2", SHA256: sha(string(bin)),
		},
		bin: bin,
	}
	units, pkg, inv := &fakeUnits{}, &fakePackages{}, newInv()
	e := New(src, units, pkg, inv, "arm64", filepath.Join(root, "data"))
	e.SetConfigRoot(filepath.Join(root, "etc-root"))
	e.SetOptRoot(filepath.Join(root, "opt"))
	return e, src, units, pkg, inv, root
}

func TestTarballInstallExtractsAndWritesUnit(t *testing.T) {
	e, _, units, pkg, inv, root := newTarExec(t)
	if err := e.Install(context.Background(), "roonbridge", "2.70.1671", nil); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(root, "opt", "roonbridge", "Bridge", "RoonBridge")
	body, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "raat") {
		t.Fatalf("extracted binary content: %q", body)
	}
	if st, err := os.Stat(binPath); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		t.Fatalf("exec bit lost: %v", st.Mode())
	}
	if _, err := os.Stat(filepath.Join(root, "opt", "roonbridge", "extra.conf")); err != nil {
		t.Fatalf("top-level file not stripped into install dir: %v", err)
	}
	unit, err := os.ReadFile(filepath.Join(root, "etc-root", "etc", "systemd", "system", "roonbridge.service"))
	if err != nil {
		t.Fatal(err)
	}
	wantExec := "ExecStart=" + filepath.Join(root, "opt", "roonbridge") + "/Bridge/RoonBridge"
	if !strings.Contains(string(unit), wantExec) {
		t.Fatalf("unit ExecStart: %s", unit)
	}
	if strings.Contains(string(unit), "{{") {
		t.Fatalf("unit template not rendered: %s", unit)
	}
	var sawReload, sawEnable, sawStart bool
	for _, c := range units.calls {
		switch c {
		case "daemon-reload":
			sawReload = true
		case "enable roonbridge.service":
			sawEnable = true
		case "start roonbridge.service":
			sawStart = true
		}
	}
	if !sawReload || !sawEnable || !sawStart {
		t.Fatalf("unit lifecycle calls: %v", units.calls)
	}
	if len(pkg.installed) != 0 {
		t.Fatalf("dpkg must not run for tar.bz2: %v", pkg.installed)
	}
	if len(pkg.deps) == 0 || pkg.deps[0] != "libasound2" {
		t.Fatalf("OS deps not installed: %v", pkg.deps)
	}
	if inv.apps["roonbridge"].Version != "2.70.1671" {
		t.Fatalf("inventory: %+v", inv.apps)
	}
}

func TestTarballUninstallRemovesOptAndUnit(t *testing.T) {
	e, _, units, pkg, inv, root := newTarExec(t)
	if err := e.Install(context.Background(), "roonbridge", "2.70.1671", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Uninstall(context.Background(), "roonbridge"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "opt", "roonbridge")); !os.IsNotExist(err) {
		t.Fatalf("opt dir survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc-root", "etc", "systemd", "system", "roonbridge.service")); !os.IsNotExist(err) {
		t.Fatalf("unit file survived: %v", err)
	}
	if len(pkg.removed) != 0 {
		t.Fatalf("apt-get remove must not run for tar.bz2 apps: %v", pkg.removed)
	}
	if _, ok := inv.apps["roonbridge"]; ok {
		t.Fatal("inventory record survived")
	}
	if units.calls[len(units.calls)-1] != "daemon-reload" {
		t.Fatalf("last call should be daemon-reload: %v", units.calls)
	}
}

func TestExtractRejectsEscapingAndLinks(t *testing.T) {
	cases := []struct {
		name string
		hdr  *tar.Header
	}{
		{"dotdot", &tar.Header{Name: "RoonBridge/../../evil.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}},
		{"symlink", &tar.Header{Name: "RoonBridge/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			if err := tw.WriteHeader(c.hdr); err != nil {
				t.Fatal(err)
			}
			if c.hdr.Typeflag == tar.TypeReg {
				tw.Write([]byte("x"))
			}
			tw.Close()
			dir := t.TempDir()
			if err := extractTarStream(tar.NewReader(&buf), dir); err == nil {
				t.Fatal("expected rejection")
			}
			ents, _ := os.ReadDir(dir)
			if len(ents) != 0 {
				t.Fatalf("files written despite rejection: %v", ents)
			}
		})
	}
}

func TestTarballIntegrityMismatch(t *testing.T) {
	e, src, _, _, _, _ := newTarExec(t)
	src.ver.SHA256 = sha("different")
	if err := e.Install(context.Background(), "roonbridge", "2.70.1671", nil); err == nil {
		t.Fatal("expected integrity failure")
	}
}
