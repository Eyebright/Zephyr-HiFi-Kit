// Package update implements version convergence for the whole product: the
// management server checks the official release feed and upgrades itself from
// signed-chain release bundles, and the server polls its terminals and drives
// them to the server's own version through the gated command channel. No
// online builds and no binary patching: upgrades replace wholesale published
// artifacts whose integrity is verified before anything is applied (NFR-S2).
package update

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"hifihub/server/internal/commands"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/license"
	"hifihub/server/internal/node"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
)

// ErrBusy reports that the play gate / in-flight jobs block a self-upgrade.
var ErrBusy = errors.New("FR-3.10：仍有终端在播放或存在未完成任务，暂缓服务端自升级")

// Options configures the update service. Restart and Executable are injectable
// so tests exercise the whole apply pipeline without touching the host.
type Options struct {
	Current      string
	FeedURL      string
	SelfEnabled  bool
	AutoApply    bool
	FleetEnabled bool
	Interval     time.Duration
	MaxParallel  int
	MaxAttempts  int
	RingPercent  int
	DistDir      string
	StageDir     string
	GOARCH       string
	Executable   string
	Restart      func() error
}

func (o *Options) defaults() {
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Interval <= 0 {
		o.Interval = 10 * time.Minute
	}
	if o.MaxParallel <= 0 {
		o.MaxParallel = 2
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.RingPercent <= 0 || o.RingPercent > 100 {
		o.RingPercent = 20
	}
	if o.FeedURL == "" {
		o.FeedURL = DefaultFeed
	}
}

// Service owns self-update and fleet convergence state.
type Service struct {
	db    *sql.DB
	nodes *node.Service
	cmds  *commands.Service
	tasks *task.Service
	lic   *license.Service
	ver   signature.Verifier
	feed  Feed
	opt   Options

	mu     sync.Mutex
	state  domain.ServerUpdateState
	policy domain.FleetUpdatePolicy
	ring   int
	rel    Release

	distMu sync.Mutex // serializes on-demand terminal package downloads
}

// New builds the update service and restores persisted state.
func New(s *store.Store, n *node.Service, c *commands.Service, t *task.Service, lic *license.Service, ver signature.Verifier, feed Feed, opt Options) *Service {
	opt.defaults()
	sv := &Service{db: s.DB, nodes: n, cmds: c, tasks: t, lic: lic, ver: ver, feed: feed, opt: opt}
	sv.state = domain.ServerUpdateState{Current: opt.Current, State: "idle", AutoApply: opt.AutoApply, FeedURL: opt.FeedURL}
	sv.policy = domain.FleetUpdatePolicy{Enabled: opt.FleetEnabled, MaxParallel: opt.MaxParallel, MaxAttempts: opt.MaxAttempts, RingPercent: opt.RingPercent}
	if raw, ok := sv.readKV("update.server"); ok {
		var st domain.ServerUpdateState
		store.FromJSON(raw, &st)
		if st.Current != "" {
			// A restart recorded before the process died means the swap landed.
			if st.State == "restarting" && st.Current == opt.Current {
				st.State = "idle"
				st.Detail = "自升级完成，已运行新版本"
				st.RestartReq = false
			}
			st.Current = opt.Current
			st.FeedURL = opt.FeedURL
			sv.state = st
		}
	}
	if raw, ok := sv.readKV("update.policy"); ok {
		var p domain.FleetUpdatePolicy
		store.FromJSON(raw, &p)
		if p.MaxParallel > 0 {
			sv.policy = p
		}
	}
	if raw, ok := sv.readKV("update.ring"); ok {
		fmt.Sscanf(raw, "%d", &sv.ring)
	}
	return sv
}

func (s *Service) readKV(key string) (string, bool) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key=?`, key).Scan(&v)
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

func (s *Service) writeKV(key, val string) {
	_, _ = s.db.Exec(`INSERT INTO server_state(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, val)
}

func (s *Service) persistState() { s.writeKV("update.server", store.ToJSON(s.state)) }

// ServerStatus returns the current self-update status.
func (s *Service) ServerStatus() domain.ServerUpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetAutoApply toggles unattended self-upgrade.
func (s *Service) SetAutoApply(v bool) domain.ServerUpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.AutoApply = v
	s.persistState()
	return s.state
}

// Policy returns the fleet convergence policy.
func (s *Service) Policy() domain.FleetUpdatePolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

// SetPolicy validates and persists the fleet convergence policy.
func (s *Service) SetPolicy(p domain.FleetUpdatePolicy) (domain.FleetUpdatePolicy, error) {
	if p.MaxParallel < 1 || p.MaxParallel > 16 {
		return p, fmt.Errorf("max_parallel 需在 1..16")
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 10 {
		return p, fmt.Errorf("max_attempts 需在 1..10")
	}
	if p.RingPercent < 5 || p.RingPercent > 100 {
		return p, fmt.Errorf("ring_percent 需在 5..100")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = p
	s.writeKV("update.policy", store.ToJSON(p))
	return s.policy, nil
}

// Check polls the feed and records whether a newer release exists.
func (s *Service) Check(ctx context.Context) (domain.ServerUpdateState, error) {
	s.mu.Lock()
	s.state.State = "checking"
	s.state.Detail = ""
	s.persistState()
	s.mu.Unlock()

	rel, err := s.feed.Latest(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CheckedAt = time.Now().Unix()
	if err != nil {
		s.state.State = "offline"
		s.state.LastError = err.Error()
		s.state.Detail = "无法访问更新源（离线或镜像不可达），不影响现有服务"
		s.persistState()
		return s.state, err
	}
	s.state.Latest = rel.Version()
	if !ValidVersion(rel.Version()) {
		s.state.State = "failed"
		s.state.LastError = "feed tag 非法: " + rel.Tag
		s.state.Detail = "更新源返回了非法版本号，已拒绝"
		s.persistState()
		return s.state, fmt.Errorf("feed tag 非法: %s", rel.Tag)
	}
	s.rel = rel
	s.state.Available = Compare(rel.Version(), s.opt.Current) > 0
	s.state.LastError = ""
	s.state.State = "idle"
	if s.state.Available {
		s.state.Detail = fmt.Sprintf("发现新版本 %s（当前 %s）", rel.Version(), s.opt.Current)
	} else {
		s.state.Detail = "已是最新版本"
	}
	s.persistState()
	return s.state, nil
}

// Tick is one polling iteration: self-check (+optional auto-apply) and fleet
// convergence. It is safe to call concurrently; the mutex serialises state.
func (s *Service) Tick(ctx context.Context) {
	s.mu.Lock()
	selfOn, auto := s.opt.SelfEnabled, s.state.AutoApply
	s.mu.Unlock()
	if selfOn {
		st, err := s.Check(ctx)
		if err == nil && auto && st.Available {
			_, _ = s.Apply(ctx)
		}
	}
	s.mu.Lock()
	fleetOn := s.policy.Enabled
	s.mu.Unlock()
	if fleetOn {
		s.fleetTick(ctx, false, false)
	}
}

// busy reports whether the play gate or in-flight jobs block a self-upgrade.
func (s *Service) busy() (bool, string) {
	ns, err := s.nodes.List()
	if err == nil {
		for _, n := range ns {
			if n.Playing && n.PlayProtect {
				return true, fmt.Sprintf("终端 %s 正在播放", n.NodeID)
			}
		}
	}
	js, err := s.tasks.List("")
	if err == nil {
		for _, j := range js {
			if !task.IsTerminal(j.State) {
				return true, fmt.Sprintf("任务 %s 仍在执行（%s）", j.ID, j.State)
			}
		}
	}
	return false, ""
}
