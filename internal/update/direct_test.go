package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	wantSHA := addAudioDeb(t, dir, "0.58", "arm64")
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.58"}}, Options{Current: "0.58", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	st, err := h.sv.UpgradeNode(context.Background(), "n1")
	if err != nil {
		t.Fatalf("UpgradeNode: %v", err)
	}
	if st.State != "upgrading" || st.Target != "0.58" || st.CommandID == "" {
		t.Fatalf("state=%+v", st)
	}
	cmds, err := h.cmds.List("n1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Type != domain.CommandAgentUpgrade {
		t.Fatalf("commands=%+v", cmds)
	}
	if cmds[0].Params.URL != "/dl/hifihub-agent_0.58_arm64_audio.deb" || cmds[0].Params.SHA256 != wantSHA {
		t.Fatalf("params=%+v", cmds[0].Params)
	}
	if _, err := h.sv.UpgradeNode(context.Background(), "n1"); !errors.Is(err, ErrUpgradeInFlight) {
		t.Fatalf("second upgrade err=%v, want in flight", err)
	}
}

func TestUpgradeNodeWithoutAudioPackageRefuses(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.58"}}, Options{Current: "0.58", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	if _, err := h.sv.UpgradeNode(context.Background(), "n1"); !errors.Is(err, ErrNodePackageMissing) {
		t.Fatalf("err=%v, want missing package", err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
}

func TestUpgradeNodeRespectsPlayGate(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	addAudioDeb(t, dir, "0.58", "arm64")
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.58"}}, Options{Current: "0.58", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, true, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	st, err := h.sv.UpgradeNode(context.Background(), "n1")
	if !errors.Is(err, ErrNodePlaying) || st.State != "waiting_idle" {
		t.Fatalf("state=%+v err=%v", st, err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
}

func TestUpgradeNodeRejectsConvergedAndOffline(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	h := newHarness(t, stubFeed{rel: Release{Tag: "v0.58"}}, Options{Current: "0.58", DistDir: dir, FleetEnabled: true})
	h.insertNode(t, "up", "arm64", domain.NodeOnline, "0.58", true, false, true)
	h.insertNode(t, "off", "arm64", domain.NodeOffline, "0.4.3", true, false, true)

	if _, err := h.sv.UpgradeNode(context.Background(), "up"); !errors.Is(err, ErrNodeAlreadyConverged) {
		t.Fatalf("converged err=%v", err)
	}
	if _, err := h.sv.UpgradeNode(context.Background(), "off"); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("offline err=%v", err)
	}
}

// distFeedServer serves a release feed whose assets are the given bytes, plus a
// release-level SHA256SUMS covering them. badSums corrupts the sums entry of the
// first asset to exercise the integrity refusal.
func distFeedServer(t *testing.T, ver string, assets map[string][]byte, badSums bool) *httptest.Server {
	t.Helper()
	var sums strings.Builder
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		sum := sha(assets[name])
		if badSums && i == 0 {
			sum = strings.Repeat("0", 64)
		}
		fmt.Fprintf(&sums, "%s  %s\n", sum, name)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		list := []map[string]any{}
		for _, name := range names {
			list = append(list, map[string]any{
				"name": name, "browser_download_url": "http://" + r.Host + "/asset/" + name,
				"digest": "sha256:" + sha(assets[name]), "size": len(assets[name]),
			})
		}
		list = append(list, map[string]any{
			"name": "SHA256SUMS", "browser_download_url": "http://" + r.Host + "/asset/SHA256SUMS",
			"size": sums.Len(),
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + ver, "assets": list})
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/asset/")
		if name == "SHA256SUMS" {
			_, _ = w.Write([]byte(sums.String()))
			return
		}
		b, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func audioTerminalBundle(t *testing.T, ver, arch string) ([]byte, string) {
	t.Helper()
	root := fmt.Sprintf("Zephyr-HiFi-Kit-terminal-%s-audio-%s", ver, arch)
	deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", ver, arch)
	debContent := "AUDIO-DEB-" + ver + "-" + arch
	innerSums := fmt.Sprintf("%s  %s\n", sha([]byte(debContent)), deb)
	data := tarGz(t, []tarEntry{
		reg(root+"/agent/"+deb, debContent),
		reg(root+"/agent/SHA256SUMS", innerSums),
	})
	return data, sha([]byte(debContent))
}

func TestUpgradeNodeDownloadsMissingAudioPackage(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	tarBytes, wantSHA := audioTerminalBundle(t, "0.58", "arm64")
	tarName := "Zephyr-HiFi-Kit-terminal-0.58-audio-arm64.tar.gz"
	srv := distFeedServer(t, "0.58", map[string][]byte{tarName: tarBytes}, false)
	h := newHarness(t, NewFeed(srv.URL+"/feed"), Options{
		Current: "0.58", FeedURL: srv.URL + "/feed", DistDir: dir,
		StageDir: filepath.Join(t.TempDir(), "stage"), FleetEnabled: true,
	})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	st, err := h.sv.UpgradeNode(context.Background(), "n1")
	if err != nil {
		t.Fatalf("UpgradeNode: %v (state=%+v)", err, st)
	}
	if st.State != "upgrading" || !strings.Contains(st.Detail, "下载") {
		t.Fatalf("state=%+v", st)
	}
	cmds, _ := h.cmds.List("n1", "")
	if len(cmds) != 1 || cmds[0].Params.URL != "/dl/hifihub-agent_0.58_arm64_audio.deb" || cmds[0].Params.SHA256 != wantSHA {
		t.Fatalf("commands=%+v", cmds)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "hifihub-agent_0.58_arm64_audio.deb")); err != nil || sha(b) != wantSHA {
		t.Fatalf("dist deb missing or wrong: %v", err)
	}
	sums := readDistSums(dir)
	if sums["hifihub-agent_0.58_arm64_audio.deb"] != wantSHA || sums["hifihub-agent_0.58_arm64.deb"] == "" {
		t.Fatalf("dist sums not upserted: %+v", sums)
	}
}

func TestUpgradeNodeDownloadsMissingManagementPackage(t *testing.T) {
	dir := t.TempDir()
	debContent := []byte("MGMT-DEB-0.58-amd64")
	debName := "Zephyr-HiFi-Kit-terminal-0.58-linux-amd64.deb"
	srv := distFeedServer(t, "0.58", map[string][]byte{debName: debContent}, false)
	h := newHarness(t, NewFeed(srv.URL+"/feed"), Options{
		Current: "0.58", FeedURL: srv.URL + "/feed", DistDir: dir,
		StageDir: filepath.Join(t.TempDir(), "stage"), FleetEnabled: true,
	})
	h.insertNode(t, "n1", "amd64", domain.NodeOnline, "0.4.3", true, false, true)

	st, err := h.sv.UpgradeNode(context.Background(), "n1")
	if err != nil {
		t.Fatalf("UpgradeNode: %v (state=%+v)", err, st)
	}
	if st.State != "upgrading" {
		t.Fatalf("state=%+v", st)
	}
	cmds, _ := h.cmds.List("n1", "")
	if len(cmds) != 1 || cmds[0].Params.SHA256 != sha(debContent) {
		t.Fatalf("commands=%+v", cmds)
	}
	if sums := readDistSums(dir); sums["hifihub-agent_0.58_amd64.deb"] != sha(debContent) {
		t.Fatalf("dist sums=%+v", sums)
	}
}

func TestUpgradeNodeDownloadRefusesVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	tarBytes, _ := audioTerminalBundle(t, "0.59", "arm64")
	srv := distFeedServer(t, "0.59", map[string][]byte{"Zephyr-HiFi-Kit-terminal-0.59-audio-arm64.tar.gz": tarBytes}, false)
	h := newHarness(t, NewFeed(srv.URL+"/feed"), Options{
		Current: "0.58", FeedURL: srv.URL + "/feed", DistDir: dir, FleetEnabled: true,
	})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	_, err := h.sv.UpgradeNode(context.Background(), "n1")
	if !errors.Is(err, ErrNodePackageMissing) || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("err=%v, want version mismatch refusal", err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
}

func TestUpgradeNodeDownloadRefusesBadReleaseSums(t *testing.T) {
	dir := t.TempDir()
	writeDistSums(t, dir, "0.58", []string{"arm64"})
	tarBytes, _ := audioTerminalBundle(t, "0.58", "arm64")
	tarName := "Zephyr-HiFi-Kit-terminal-0.58-audio-arm64.tar.gz"
	srv := distFeedServer(t, "0.58", map[string][]byte{tarName: tarBytes}, true)
	h := newHarness(t, NewFeed(srv.URL+"/feed"), Options{
		Current: "0.58", FeedURL: srv.URL + "/feed", DistDir: dir,
		StageDir: filepath.Join(t.TempDir(), "stage"), FleetEnabled: true,
	})
	h.insertNode(t, "n1", "arm64", domain.NodeOnline, "0.4.3", true, false, true)
	setNodeFlavor(t, h, "n1", FlavorAudio)

	_, err := h.sv.UpgradeNode(context.Background(), "n1")
	if !errors.Is(err, ErrNodePackageMissing) {
		t.Fatalf("err=%v, want integrity refusal", err)
	}
	if cmds, _ := h.cmds.List("n1", ""); len(cmds) != 0 {
		t.Fatalf("unexpected command: %+v", cmds)
	}
	if _, err := os.Stat(filepath.Join(dir, "hifihub-agent_0.58_arm64_audio.deb")); !os.IsNotExist(err) {
		t.Fatalf("tampered package must not land in dist: %v", err)
	}
}
