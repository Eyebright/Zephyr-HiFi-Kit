package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestImportLocalVersion(t *testing.T) {
	base := setup(t, "dev")
	if s, b := doJSON(t, "POST", base+"/apps", map[string]any{"app_id": "roonbridge", "name": "Roon Bridge", "category": "renderer"}); s != 201 {
		t.Fatalf("create app: %d %s", s, b)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "RoonBridge_linuxarmv8.tar.bz2")
	body := []byte("vendor-tarball-bytes")
	if err := os.WriteFile(pkg, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	s, b := doJSON(t, "POST", base+"/apps/roonbridge/versions/import-local", map[string]any{
		"version": "2.70.1671", "arch": "arm64", "format": "tar.bz2",
		"local_path": pkg, "units": []string{"roonbridge.service"},
	})
	if s != 201 {
		t.Fatalf("import: %d %s", s, b)
	}
	v := mustJSON(t, b)
	if v["sha256"] != want || v["format"] != "tar.bz2" || v["signed"] != true {
		t.Fatalf("version fields: %s", b)
	}
	if _, b := doJSON(t, "GET", base+"/apps/roonbridge/versions", nil); !bytes.Contains(b, []byte("2.70.1671")) {
		t.Fatalf("version not listed: %s", b)
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"relative path", map[string]any{"version": "1", "arch": "arm64", "format": "deb", "local_path": "relative.deb"}},
		{"missing file", map[string]any{"version": "1", "arch": "arm64", "format": "deb", "local_path": filepath.Join(dir, "nope.deb")}},
		{"directory", map[string]any{"version": "1", "arch": "arm64", "format": "deb", "local_path": dir}},
		{"bad format", map[string]any{"version": "1", "arch": "arm64", "format": "apt", "local_path": pkg}},
		{"empty version", map[string]any{"arch": "arm64", "format": "deb", "local_path": pkg}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s, b := doJSON(t, "POST", base+"/apps/roonbridge/versions/import-local", c.body); s != 400 {
				t.Fatalf("got %d want 400 (%s)", s, b)
			}
		})
	}
	if s, _ := doJSON(t, "POST", base+"/apps/missing/versions/import-local", map[string]any{
		"version": "1", "arch": "arm64", "format": "deb", "local_path": pkg,
	}); s != 404 {
		t.Fatalf("unknown app: %d", s)
	}
}

func TestImportLocalEnforceRequiresSignature(t *testing.T) {
	base := setup(t, "enforce")
	doJSON(t, "POST", base+"/apps", map[string]any{"app_id": "roonbridge", "name": "Roon Bridge", "category": "renderer"})
	pkg := filepath.Join(t.TempDir(), "vendor.tar.bz2")
	os.WriteFile(pkg, []byte("x"), 0o644)
	s, b := doJSON(t, "POST", base+"/apps/roonbridge/versions/import-local", map[string]any{
		"version": "2.70.1671", "arch": "arm64", "format": "tar.bz2", "local_path": pkg,
	})
	if s != 400 {
		t.Fatalf("enforce unsigned: %d %s", s, b)
	}
}
