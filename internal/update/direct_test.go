package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hifihub/server/internal/domain"
)

func setNodeFlavor(t *testing.T, h *harness, id, flavor string) {
	t.Helper()
	if _, err := h.st.DB.Exec(`UPDATE nodes SET build_flavor=? WHERE node_id=?`, flavor, id); err != nil {
		t.Fatal(err)
	}
}

func addAudioDeb(t *testing.T, dir, ver, arch string) string {
	t.Helper()
	name := "hifihub-agent_" + ver + "_" + arch + "_audio.deb"
	content := []byte("AUDIO-" + ver + "-" + arch)
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	line := sha(content) + "  " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), append(raw, line...), 0o644); err != nil {
		t.Fatal(err)
	}
	return sha(content)
}

func TestUpgradeNodeUsesAudioPackage(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.51", []string{"arm64"})
	wantSHA := addAudioDeb(t, dir, "0.51", "arm64")
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.51"}}, Options{Current: "0.51", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	st, err := h.sv.UpgradeNode("n1")
	if err != nil {
		t.Fatalf("UpgradeNode: %v", err)
	}
	if st.State != "upgrading" || st.Target != "0.51" || st.CommandID == "" {
		t.Fatalf("state=%+v", st)
	}
	cmds, err := h.cmds.List("n1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Type != domain.CommandAgentUpgrade {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].Params.URL != "/dl/hifihub-agent_0.51_arm64_audio.deb" || cmds[0].Params.SHA256 != wantSHA {
		t.Fatalf("params=%+v", cmds[0].Params)
	}
	if _, err := h.sv.UpgradeNode("n1"); !errors.Is(err, ErrUpgradeInFlight) {
		t.Fatalf("second upgrade err=%v, want in flight", err)
	}
}

func TestUpgradeNodeWithoutAudioPackageRefuses(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.51", []string{"arm64"})
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.51"}}, Options{Current: "0.51", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	if _, err := h.sv.UpgradeNode("n1"); !errors.Is(err, ErrNodePackageMissing) {
		t.Fatalf("err=%v, want missing package", err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
}

func TestUpgradeNodeRespectsPlayGate(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.51", []string{"arm64"})
	addAudioDeb(t, dir, "0.51", "arm64")
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.51"}}, Options{Current: "0.51", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, true, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	st, err := h.sv.UpgradeNode("n1")
	if !errors.Is(err, ErrNodePlaying) || st.State != "waiting_idle" {
		t.Fatalf("state=%+v err=%v", st, err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
}

func TestUpgradeNodeRejectsConvergedAndOffline(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.51", []string{"arm64"})
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.51"}}, Options{Current: "0.51", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "up", "arm64", domain.NodeOnline, "0.51", true, false, true)
	h.insertNode(t, "off", "arm64", domain.NodeOffline, "0.4.3", true, false, true)

	if _, err := h.sv.UpgradeNode("up"); !errors.Is(err, ErrNodeAlreadyConverged) {
		t.Fatalf("converged err=%v", err)
	}
	if _, err := h.sv.UpgradeNode("off"); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("offline err=%v", err)
	}
}
