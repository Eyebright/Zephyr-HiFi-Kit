package update

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hifihub/server/internal/domain"
)

var (
	ErrNodeOffline          = errors.New("终端离线，无法升级")
	ErrNodeUnauthorized     = errors.New("终端未授权，无法升级")
	ErrNodeArchUnsupported  = errors.New("终端架构不支持自动升级")
	ErrNodePackageMissing   = errors.New("服务端缺少对应构建类型的终端安装包")
	ErrNodePlaying          = errors.New("FR-3.10：终端正在播放，空闲后再升级")
	ErrUpgradeInFlight      = errors.New("终端升级已在进行中")
	ErrUpgradeUnavailable   = errors.New("更新服务未启用")
	ErrNodeAlreadyConverged = errors.New("终端已是目标版本")
)

// UpgradeNode issues one agent-upgrade command for a specific terminal,
// independent of the ring policy. The command is idempotent while an upgrade
// is in flight and never bypasses play protection. A missing distribution
// package is fetched from the release feed on demand instead of refusing.
func (s *Service) UpgradeNode(ctx context.Context, nodeID string) (domain.NodeUpdateState, error) {
	if s == nil || s.nodes == nil || s.cmds == nil {
		return domain.NodeUpdateState{}, ErrUpgradeUnavailable
	}
	n, err := s.nodes.Get(nodeID)
	if err != nil {
		return domain.NodeUpdateState{}, err
	}
	target := s.opt.Current
	st := domain.NodeUpdateState{NodeID: n.NodeID, Current: n.AgentVersion, Target: target}
	switch {
	case n.Status != domain.NodeOnline:
		st.State = "offline"
		st.Detail = ErrNodeOffline.Error()
		return st, ErrNodeOffline
	case !n.Authorized:
		st.State = "unsupported"
		st.Detail = ErrNodeUnauthorized.Error()
		return st, ErrNodeUnauthorized
	case n.AgentVersion == target:
		st.State = "converged"
		return st, ErrNodeAlreadyConverged
	case !fleetArchs[n.Arch]:
		st.State = "unsupported"
		st.Detail = ErrNodeArchUnsupported.Error()
		return st, ErrNodeArchUnsupported
	}

	sums := readDistSums(s.opt.DistDir)
	deb := debName(target, n.Arch, n.BuildFlavor)
	downloaded := false
	if sums[deb] == "" {
		sum, err := s.ensureDistPackage(ctx, target, n.Arch, n.BuildFlavor)
		if err != nil {
			st.State = "unsupported"
			st.Detail = "自动下载终端包失败：" + err.Error()
			return st, fmt.Errorf("%w：%s", ErrNodePackageMissing, st.Detail)
		}
		sums[deb] = sum
		downloaded = true
	}
	if n.Playing && n.PlayProtect {
		st.State = "waiting_idle"
		st.Detail = ErrNodePlaying.Error()
		return st, ErrNodePlaying
	}

	s.mu.Lock()
	row := s.loadRows()[n.NodeID]
	if row.State == "upgrading" {
		s.mu.Unlock()
		return row, ErrUpgradeInFlight
	}
	s.mu.Unlock()

	cmd, err := s.cmds.Create(n.NodeID, domain.CommandAgentUpgrade, domain.CommandParams{
		Version: target, Arch: n.Arch, SHA256: sums[deb], URL: "/dl/" + deb,
	})
	if err != nil {
		return st, err
	}
	st.State = "upgrading"
	st.Attempts = 1
	st.LastAttempt = time.Now().Unix()
	st.CommandID = cmd.ID
	st.Detail = "已下发 agent-upgrade，等待终端执行"
	if downloaded {
		st.Detail = "已从更新源下载终端包并下发 agent-upgrade，等待终端执行"
	}
	s.mu.Lock()
	s.saveRow(st)
	s.mu.Unlock()
	return st, nil
}
