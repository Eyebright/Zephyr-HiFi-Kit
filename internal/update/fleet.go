package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hifihub/server/internal/domain"
)

// FleetReport is the convergence snapshot rendered by the console.
type FleetReport struct {
	Target   string                   `json:"target"`
	Ring     int                      `json:"ring"`
	RingSize int                      `json:"ring_size"`
	Policy   domain.FleetUpdatePolicy `json:"policy"`
	Nodes    []domain.NodeUpdateState `json:"nodes"`
	Counts   map[string]int           `json:"counts"`
}

var fleetArchs = map[string]bool{"amd64": true, "arm64": true, "armhf": true}

// FleetStatus computes the convergence snapshot without issuing commands.
func (s *Service) FleetStatus() FleetReport { return s.fleetTick(context.Background(), true, false) }

// Converge forces one immediate convergence pass (ignores retry backoff).
func (s *Service) Converge(ctx context.Context) FleetReport { return s.fleetTick(ctx, false, true) }

func (s *Service) fleetTick(ctx context.Context, dryRun, force bool) FleetReport {
	s.mu.Lock()
	policy := s.policy
	ring := s.ring
	target := s.opt.Current
	s.mu.Unlock()

	rep := FleetReport{Target: target, Policy: policy, Nodes: []domain.NodeUpdateState{}, Counts: map[string]int{}}
	ns, err := s.nodes.List()
	if err != nil {
		return rep
	}
	sums := readDistSums(s.opt.DistDir)
	rows := s.loadRows()
	now := time.Now().Unix()

	for _, n := range ns {
		row := rows[n.NodeID]
		row.NodeID = n.NodeID
		row.Current = n.AgentVersion
		row.Target = target
		deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", target, n.Arch)
		switch {
		case n.Status != domain.NodeOnline:
			row.State = "offline"
			row.Detail = "终端离线，回连后自动继续"
		case !n.Authorized:
			row.State = "unsupported"
			row.Detail = "终端未授权，先在控制台授权该终端"
		case n.AgentVersion == target:
			row.State = "converged"
			row.Detail = ""
			row.Attempts = 0
			row.CommandID = ""
		case !fleetArchs[n.Arch]:
			row.State = "unsupported"
			row.Detail = "架构 " + n.Arch + " 不在自动更新范围（安卓经 APK 升级）"
		case sums[deb] == "":
			row.State = "unsupported"
			row.Detail = "服务端 /dl/ 缺少 " + deb + "，请先自升级或补齐分发包"
		case n.Playing && n.PlayProtect:
			row.State = "waiting_idle"
			row.Detail = "FR-3.10：播放中，空闲后自动升级"
		default:
			s.reconcileCommand(&row, now)
			// Suspend only after failures settle: an in-flight command must get
			// its chance to finish even when the attempt counter hit the cap.
			if row.State == "pending" && row.Attempts >= policy.MaxAttempts {
				row.State = "suspended"
				row.Detail = fmt.Sprintf("连续 %d 次失败已挂起，可在控制台重试", row.Attempts)
			}
		}
		rows[n.NodeID] = row
		s.saveRow(row)
		rep.Nodes = append(rep.Nodes, row)
		rep.Counts[row.State]++
	}
	sort.Slice(rep.Nodes, func(i, j int) bool { return rep.Nodes[i].NodeID < rep.Nodes[j].NodeID })

	// Ring maths run over participants (online, authorized, supported arch);
	// converged members stay in the list so a ring only advances once every
	// member of it reports the target version.
	participants := []domain.NodeUpdateState{}
	for _, r := range rep.Nodes {
		if r.State == "offline" || r.State == "unsupported" {
			continue
		}
		participants = append(participants, r)
	}
	ringSize := (len(participants)*policy.RingPercent + 99) / 100
	if ringSize < 1 {
		ringSize = 1
	}
	if ring*ringSize >= len(participants) {
		ring = 0
	}
	lo := ring * ringSize
	hi := lo + ringSize
	if hi > len(participants) {
		hi = len(participants)
	}
	batch := participants[lo:hi]
	allConverged := len(batch) > 0
	inFlight := 0
	for _, r := range rep.Nodes {
		if r.State == "upgrading" {
			inFlight++
		}
	}
	for _, r := range batch {
		if r.State != "converged" {
			allConverged = false
		}
		if dryRun || r.State != "pending" || inFlight >= policy.MaxParallel {
			continue
		}
		if !force && now-r.LastAttempt < backoffSeconds(r.Attempts) {
			continue
		}
		n, err := s.nodes.Get(r.NodeID)
		if err != nil {
			continue
		}
		deb := fmt.Sprintf("hifihub-agent_%s_%s.deb", target, n.Arch)
		cmd, err := s.cmds.Create(n.NodeID, domain.CommandAgentUpgrade, domain.CommandParams{
			Version: target, Arch: n.Arch, SHA256: sums[deb], URL: "/dl/" + deb,
		})
		if err != nil {
			r.State = "pending"
			r.Detail = err.Error()
			rows[r.NodeID] = r
			s.saveRow(r)
			continue
		}
		r.State = "upgrading"
		r.Attempts++
		r.LastAttempt = now
		r.CommandID = cmd.ID
		r.Detail = "已下发 agent-upgrade，等待终端执行"
		rows[r.NodeID] = r
		s.saveRow(r)
		inFlight++
	}
	if allConverged {
		ring++
	}
	if len(participants) == 0 {
		ring = 0
	}
	s.mu.Lock()
	s.ring = ring
	s.writeKV("update.ring", fmt.Sprintf("%d", ring))
	s.mu.Unlock()
	rep.Ring = ring
	rep.RingSize = ringSize
	// Refresh states mutated during issuance.
	for i := range rep.Nodes {
		rep.Nodes[i] = rows[rep.Nodes[i].NodeID]
	}
	rep.Counts = map[string]int{}
	for _, r := range rep.Nodes {
		rep.Counts[r.State]++
	}
	return rep
}

// reconcileCommand maps the tracked command's outcome onto the row state. A
// successful install only counts as converged once the heartbeat reports the
// target version, so a restart that never comes back is never marked done.
func (s *Service) reconcileCommand(row *domain.NodeUpdateState, now int64) {
	if row.CommandID == "" {
		pending, err := s.cmds.List(row.NodeID, "pending")
		if err == nil {
			for _, c := range pending {
				if c.Type == domain.CommandAgentUpgrade {
					row.CommandID = c.ID
					break
				}
			}
		}
	}
	if row.CommandID == "" {
		row.State = "pending"
		row.Detail = "等待服务端下发升级指令"
		return
	}
	c, err := s.cmds.Get(row.CommandID)
	if err != nil {
		row.State = "pending"
		row.CommandID = ""
		row.Detail = "等待服务端下发升级指令"
		return
	}
	if c.Status != "done" {
		row.State = "upgrading"
		row.Detail = "终端执行中"
		return
	}
	if c.ResultStatus == "ok" {
		row.State = "upgrading"
		row.Detail = "安装完成，等待重启后心跳确认版本"
		return
	}
	row.State = "pending"
	row.CommandID = ""
	row.Detail = tailLog(c.Log)
}

func backoffSeconds(attempts int) int64 {
	if attempts <= 0 {
		return 0
	}
	return int64(60 * attempts)
}

func tailLog(log string) string {
	log = strings.TrimSpace(log)
	if r := []rune(log); len(r) > 240 {
		log = "…" + string(r[len(r)-240:])
	}
	if log == "" {
		return "终端上报失败"
	}
	return log
}

func readDistSums(dir string) map[string]string {
	out := map[string]string{}
	if dir == "" {
		return out
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) >= 2 {
			out[strings.TrimPrefix(parts[1], "*")] = parts[0]
		}
	}
	return out
}

func (s *Service) loadRows() map[string]domain.NodeUpdateState {
	out := map[string]domain.NodeUpdateState{}
	rows, err := s.db.Query(`SELECT node_id,target,state,attempts,last_attempt,command_id,detail FROM update_nodes`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r domain.NodeUpdateState
		if err := rows.Scan(&r.NodeID, &r.Target, &r.State, &r.Attempts, &r.LastAttempt, &r.CommandID, &r.Detail); err == nil {
			out[r.NodeID] = r
		}
	}
	return out
}

func (s *Service) saveRow(r domain.NodeUpdateState) {
	_, _ = s.db.Exec(`INSERT INTO update_nodes(node_id,target,state,attempts,last_attempt,command_id,detail)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET target=excluded.target,state=excluded.state,
		attempts=excluded.attempts,last_attempt=excluded.last_attempt,
		command_id=excluded.command_id,detail=excluded.detail`,
		r.NodeID, r.Target, r.State, r.Attempts, r.LastAttempt, r.CommandID, r.Detail)
}

// RetryNode clears a suspended terminal's failure counter so the next pass
// picks it up again.
func (s *Service) RetryNode(nodeID string) error {
	res, err := s.db.Exec(`UPDATE update_nodes SET attempts=0,state='pending',command_id='',detail='' WHERE node_id=?`, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("终端 %s 没有更新记录", nodeID)
	}
	return nil
}
