package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureDistPackage fills a missing /dl/ terminal package by downloading the
// matching release asset from the feed. Integrity chain: the feed-published
// digest (when present), the release-level SHA256SUMS over the downloaded
// asset, and — for audio bundles — the bundle's own SHA256SUMS over the
// extracted agent tree. The deb is then registered in the distribution
// SHA256SUMS so fleet convergence and the /dl/ handler see the same bytes.
func (s *Service) ensureDistPackage(ctx context.Context, version, arch, flavor string) (string, error) {
	if s.opt.DistDir == "" {
		return "", fmt.Errorf("未配置终端分发目录")
	}
	s.distMu.Lock()
	defer s.distMu.Unlock()
	deb := debName(version, arch, flavor)
	if sum := readDistSums(s.opt.DistDir)[deb]; sum != "" {
		return sum, nil
	}
	rel, err := s.feed.Latest(ctx)
	if err != nil {
		return "", fmt.Errorf("无法访问更新源：%w", err)
	}
	if rel.Version() != version {
		return "", fmt.Errorf("更新源最新版本 %s 与目标版本 %s 不一致，请先升级服务端", rel.Version(), version)
	}
	assetName := fmt.Sprintf("Zephyr-HiFi-Kit-terminal-%s-linux-%s.deb", version, arch)
	if flavor == FlavorAudio {
		assetName = fmt.Sprintf("Zephyr-HiFi-Kit-terminal-%s-audio-%s.tar.gz", version, arch)
	}
	asset, ok := rel.Asset(assetName)
	if !ok {
		return "", fmt.Errorf("更新源缺少资产 %s", assetName)
	}
	sumsAsset, ok := rel.Asset("SHA256SUMS")
	if !ok {
		return "", fmt.Errorf("更新源缺少 SHA256SUMS，无法校验终端包")
	}
	stage := s.opt.StageDir
	if stage == "" {
		stage = filepath.Join(os.TempDir(), "hifihub-update")
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}
	assetPath := filepath.Join(stage, assetName)
	got, err := fetchAsset(ctx, s.opt.FeedURL, asset, assetPath)
	if err != nil {
		return "", err
	}
	sumsPath := filepath.Join(stage, "SHA256SUMS-"+version)
	if _, err := fetchAsset(ctx, s.opt.FeedURL, sumsAsset, sumsPath); err != nil {
		return "", err
	}
	if err := verifyReleaseSum(sumsPath, assetName, got); err != nil {
		return "", err
	}
	src := assetPath
	if flavor == FlavorAudio {
		extractDir := filepath.Join(stage, fmt.Sprintf("terminal-%s-audio-%s", version, arch))
		if err := os.RemoveAll(extractDir); err != nil {
			return "", err
		}
		if err := extractTarGz(assetPath, extractDir); err != nil {
			return "", err
		}
		root := filepath.Join(extractDir, fmt.Sprintf("Zephyr-HiFi-Kit-terminal-%s-audio-%s", version, arch))
		agentDir := filepath.Join(root, "agent")
		if err := verifySums(agentDir, filepath.Join(agentDir, "SHA256SUMS")); err != nil {
			return "", err
		}
		src = filepath.Join(agentDir, fmt.Sprintf("hifihub-agent_%s_%s.deb", version, arch))
	}
	debSum, err := fileSHA256(src)
	if err != nil {
		return "", err
	}
	if err := copyFile(src, filepath.Join(s.opt.DistDir, deb), 0o644); err != nil {
		return "", err
	}
	if err := upsertDistSum(s.opt.DistDir, deb, debSum); err != nil {
		return "", err
	}
	return debSum, nil
}

// verifyReleaseSum checks one downloaded asset against the release-level
// SHA256SUMS (which lists every published asset by bare filename).
func verifyReleaseSum(sumsPath, name, got string) error {
	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) >= 2 && strings.TrimPrefix(parts[1], "*") == name {
			if !strings.EqualFold(parts[0], got) {
				return fmt.Errorf("发布校验和不匹配：%s got %s want %s", name, got, parts[0])
			}
			return nil
		}
	}
	return fmt.Errorf("发布 SHA256SUMS 未列出 %s", name)
}

// upsertDistSum adds or replaces one filename's entry in the distribution
// SHA256SUMS, preserving all other lines. The rewrite is atomic so the /dl/
// handler never reads a half-written sums file.
func upsertDistSum(dir, name, sum string) error {
	path := filepath.Join(dir, "SHA256SUMS")
	raw, _ := os.ReadFile(path)
	var out strings.Builder
	replaced := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 && strings.TrimPrefix(parts[1], "*") == name {
			out.WriteString(sum + "  " + name + "\n")
			replaced = true
			continue
		}
		out.WriteString(trimmed + "\n")
	}
	if !replaced {
		out.WriteString(sum + "  " + name + "\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
