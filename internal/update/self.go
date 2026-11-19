package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"hifihub/server/internal/domain"
)

// Apply downloads the newest release bundle, verifies it end to end (feed
// digest, optional detached GPG signature against the imported trust root, and
// the bundle's own SHA256SUMS over every extracted file), refreshes the agent
// distribution set so terminals can converge to the same version, atomically
// replaces the running server binary and restarts the unit. It refuses to run
// while any terminal is playing under play protection or a job is in flight.
func (s *Service) Apply(ctx context.Context) (domain.ServerUpdateState, error) {
	st, err := s.Check(ctx)
	if err != nil {
		return st, err
	}
	if !st.Available {
		return st, fmt.Errorf("当前已是最新版本 %s", s.opt.Current)
	}
	s.mu.Lock()
	if s.state.State == "applying" || s.state.State == "restarting" {
		cur := s.state
		s.mu.Unlock()
		return cur, fmt.Errorf("自升级正在进行中")
	}
	if busy, why := s.busy(); busy {
		s.state.State = "waiting_idle"
		s.state.Detail = why + "；空闲后自动继续"
		s.persistState()
		cur := s.state
		s.mu.Unlock()
		return cur, ErrBusy
	}
	s.state.State = "applying"
	s.state.Detail = "下载并校验 " + st.Latest
	s.persistState()
	ver := st.Latest
	rel := s.rel
	s.mu.Unlock()

	fail := func(e error) (domain.ServerUpdateState, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.State = "failed"
		s.state.LastError = e.Error()
		s.state.Detail = "自升级失败：" + e.Error()
		s.persistState()
		return s.state, e
	}

	tarName := fmt.Sprintf("Zephyr-HiFi-Kit-server-%s.tar.gz", ver)
	asset, ok := rel.Asset(tarName)
	if !ok {
		return fail(fmt.Errorf("更新源缺少资产 %s", tarName))
	}
	stage := s.opt.StageDir
	if stage == "" {
		stage = filepath.Join(os.TempDir(), "hifihub-update")
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return fail(err)
	}
	tarPath := filepath.Join(stage, tarName)
	if _, err := fetchAsset(ctx, s.opt.FeedURL, asset, tarPath); err != nil {
		return fail(err)
	}
	// A published detached signature is mandatory to verify when present; the
	// server never applies bytes that fail the imported trust root.
	if asc, ok := rel.Asset(tarName + ".asc"); ok {
		ascPath := tarPath + ".asc"
		if _, err := fetchAsset(ctx, s.opt.FeedURL, asc, ascPath); err != nil {
			return fail(err)
		}
		if s.ver != nil {
			if err := s.ver.Verify(tarPath, ascPath); err != nil {
				return fail(fmt.Errorf("GPG 验签失败：%w", err))
			}
		}
	}
	extractDir := filepath.Join(stage, "extracted-"+ver)
	if err := os.RemoveAll(extractDir); err != nil {
		return fail(err)
	}
	if err := extractTarGz(tarPath, extractDir); err != nil {
		return fail(err)
	}
	root := filepath.Join(extractDir, "Zephyr-HiFi-Kit-server-"+ver)
	if err := verifySums(root, filepath.Join(root, "SHA256SUMS")); err != nil {
		return fail(err)
	}
	binSrc := filepath.Join(root, "server", "hifihub-server-linux-"+s.opt.GOARCH)
	if _, err := os.Stat(binSrc); err != nil {
		return fail(fmt.Errorf("bundle 缺少 %s 二进制: %w", s.opt.GOARCH, err))
	}
	// Refresh /dl/ so the fleet converges to exactly this server version.
	if s.opt.DistDir != "" {
		if err := copyTree(filepath.Join(root, "server", "dist"), s.opt.DistDir); err != nil {
			return fail(fmt.Errorf("刷新终端分发目录失败：%w", err))
		}
	}
	exe := s.opt.Executable
	if exe == "" {
		if e, err := os.Executable(); err == nil {
			exe = e
		}
	}
	if exe != "" {
		if err := swapBinary(binSrc, exe); err != nil {
			return fail(fmt.Errorf("置换二进制失败：%w", err))
		}
	}

	s.mu.Lock()
	s.state.StagedAt = time.Now().Unix()
	s.state.RestartReq = true
	s.state.LastError = ""
	if s.opt.Restart != nil {
		s.state.State = "restarting"
		s.state.Detail = "已置换二进制并刷新终端分发包，正在重启服务"
	} else {
		s.state.State = "staged"
		s.state.Detail = "已置换二进制并刷新终端分发包，等待重启服务生效"
	}
	s.persistState()
	cur := s.state
	restart := s.opt.Restart
	s.mu.Unlock()

	if restart != nil {
		if err := restart(); err != nil {
			return fail(fmt.Errorf("重启服务失败：%w", err))
		}
	}
	return cur, nil
}

// swapBinary writes the new binary beside the target then renames over it, so
// a crash mid-copy can never leave a half-written executable in place.
func swapBinary(src, dst string) error {
	tmp := dst + ".new"
	if err := copyFile(src, tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// copyTree copies the regular files of src into dst (flat dist set).
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), 0o644); err != nil {
			return err
		}
	}
	return nil
}
