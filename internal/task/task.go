// Package task implements the provision/upgrade/rollback state machine and the
// cross-protocol play-protection gate (FR-3.5 / FR-3.10 / doc 7.2).
package task

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
)

// ErrNodeUnauthorized rejects jobs against nodes without an authorized seat.
var ErrNodeUnauthorized = errors.New("终端未授权：请先在控制台授权该终端")

// Service orchestrates jobs.
type Service struct {
	db          *sql.DB
	nodes       *node.Service
	boms        *bom.Service
	cat         *catalog.Service
	playGate    bool
	idleTimeout time.Duration
}

// New creates a task service.
func New(s *store.Store, n *node.Service, b *bom.Service, c *catalog.Service, playGate bool, idleTimeout time.Duration) *Service {
	return &Service{db: s.DB, nodes: n, boms: b, cat: c, playGate: playGate, idleTimeout: idleTimeout}
}

// executablePhases are the ordered steps agent and server drive (doc 7.2).
var executablePhases = []string{
	domain.JobPrecheck, domain.JobResolveDeps, domain.JobDownloading, domain.JobVerifying,
	domain.JobInstalling, domain.JobRenderingConfig, domain.JobEnablingUnits, domain.JobValidating,
}

// CreateJob queues a new job against a published BOM.
func (s *Service) CreateJob(nodeID, bomID string, bomVersion int, kind string) (domain.Job, error) {
	var j domain.Job
	n, err := s.nodes.Get(nodeID)
	if err != nil {
		return j, fmt.Errorf("node: %w", err)
	}
	// Seat gate: the node must hold an authorized seat (built-in grant or
	// licensed) before any job can be queued.
	if !n.Authorized {
		return j, ErrNodeUnauthorized
	}
	var b domain.Bom
	if bomVersion > 0 {
		b, err = s.boms.Get(bomID, bomVersion)
	} else {
		b, err = s.boms.Latest(bomID)
	}
	if err != nil {
		return j, fmt.Errorf("bom: %w", err)
	}
	if b.Status != domain.BomPublished {
		return j, fmt.Errorf("BOM %s v%d is not published", b.BomID, b.Version)
	}
	if kind == "" {
		kind = domain.KindProvision
	}
	j = domain.Job{
		ID:        "job-" + uuid.NewString(),
		NodeID:    n.NodeID,
		BomID:     b.BomID,
		Kind:      kind,
		State:     domain.JobQueued,
		CreatedAt: time.Now().Unix(),
	}
	for _, p := range executablePhases {
		j.Phases = append(j.Phases, domain.Phase{Name: p, State: "pending"})
	}
	if err := s.insert(j); err != nil {
		return j, err
	}
	return j, nil
}

func (s *Service) insert(j domain.Job) error {
	_, err := s.db.Exec(`INSERT INTO jobs(id,node_id,bom_id,kind,state,phases,reason,created_at,started_at,finished_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.NodeID, j.BomID, j.Kind, j.State, store.ToJSON(j.Phases), j.Reason,
		j.CreatedAt, j.StartedAt, j.FinishedAt)
	return err
}

func (s *Service) save(j domain.Job) error {
	_, err := s.db.Exec(`UPDATE jobs SET state=?,phases=?,reason=?,started_at=?,finished_at=? WHERE id=?`,
		j.State, store.ToJSON(j.Phases), j.Reason, j.StartedAt, j.FinishedAt, j.ID)
	return err
}

// Get returns one job.
func (s *Service) Get(id string) (domain.Job, error) {
	row := s.db.QueryRow(`SELECT id,node_id,COALESCE(bom_id,''),kind,state,COALESCE(phases,'[]'),
		COALESCE(reason,''),created_at,COALESCE(started_at,0),COALESCE(finished_at,0) FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// List returns jobs, optionally filtered by node.
func (s *Service) List(nodeID string) ([]domain.Job, error) {
	q := `SELECT id,node_id,COALESCE(bom_id,''),kind,state,COALESCE(phases,'[]'),
		COALESCE(reason,''),created_at,COALESCE(started_at,0),COALESCE(finished_at,0) FROM jobs`
	args := []any{}
	if nodeID != "" {
		q += ` WHERE node_id=?`
		args = append(args, nodeID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJob(row interface{ Scan(...any) error }) (domain.Job, error) {
	var j domain.Job
	var phases string
	err := row.Scan(&j.ID, &j.NodeID, &j.BomID, &j.Kind, &j.State, &phases, &j.Reason,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		return j, err
	}
	store.FromJSON(phases, &j.Phases)
	return j, nil
}

// Advance drives the server-side phases (precheck, dep resolution) and then
// applies the play gate. When any protocol is playing the job parks in
// waiting_idle and NO service-affecting step runs (FR-3.10). When idle it
// reaches downloading, where the agent takes over.
func (s *Service) Advance(id string) (domain.Job, error) {
	j, err := s.Get(id)
	if err != nil {
		return j, err
	}
	if isTerminal(j.State) {
		return j, nil
	}
	if j.StartedAt == 0 {
		j.StartedAt = time.Now().Unix()
	}

	switch j.State {
	case domain.JobQueued, domain.JobPrecheck:
		if err := s.precheck(&j); err != nil {
			return s.fail(&j, err)
		}
		s.setPhase(&j, domain.JobPrecheck, "ok", "")
		j.State = domain.JobResolveDeps
		fallthrough
	case domain.JobResolveDeps:
		closure, err := s.resolveDeps(&j)
		if err != nil {
			return s.fail(&j, err)
		}
		s.setPhase(&j, domain.JobResolveDeps, "ok", strings.Join(closure, ","))
		// Play-protection gate (FR-3.10.1): all four protocols must be idle,
		// unless the node opted out via play_protect=false.
		if s.playGate {
			n, err := s.nodes.Get(j.NodeID)
			if err != nil {
				return s.fail(&j, err)
			}
			if n.Playing && n.PlayProtect {
				j.State = domain.JobWaitingIdle
				j.Reason = "waiting for all protocols to be idle (FR-3.10)"
				return j, s.save(j)
			}
		}
		j.State = domain.JobDownloading
		s.setPhase(&j, domain.JobDownloading, "running", "awaiting agent pull")
		return j, s.save(j)
	case domain.JobWaitingIdle:
		n, err := s.nodes.Get(j.NodeID)
		if err != nil {
			return s.fail(&j, err)
		}
		if n.Playing && n.PlayProtect {
			if time.Now().Unix()-j.StartedAt > int64(s.idleTimeout.Seconds()) {
				j.State = domain.JobSuspended
				j.Reason = "idle wait timeout; suspended (FR-3.10.2)"
				return j, s.save(j)
			}
			return j, nil // still playing; keep waiting
		}
		j.State = domain.JobDownloading
		j.Reason = ""
		s.setPhase(&j, domain.JobDownloading, "running", "awaiting agent pull")
		return j, s.save(j)
	default:
		// Agent-driven phases advance via ReportPhase.
		return j, nil
	}
}

func (s *Service) precheck(j *domain.Job) error {
	n, err := s.nodes.Get(j.NodeID)
	if err != nil {
		return err
	}
	b, err := s.boms.Latest(j.BomID)
	if err != nil {
		return err
	}
	rep := s.boms.Validate(b, n.Arch)
	if !rep.OK() {
		return fmt.Errorf("precheck: %s", strings.Join(rep.Errors, "; "))
	}
	return nil
}

func (s *Service) resolveDeps(j *domain.Job) ([]string, error) {
	n, err := s.nodes.Get(j.NodeID)
	if err != nil {
		return nil, err
	}
	b, err := s.boms.Latest(j.BomID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, ba := range b.Apps {
		v, err := s.cat.ResolveVersion(ba.AppID, ba.Version, n.Arch)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", ba.AppID, err)
		}
		for _, p := range v.DepsPkgs {
			set[p] = true
		}
	}
	out := []string{}
	for p := range set {
		out = append(out, p)
	}
	return out, nil
}

// ReportPhase lets the agent report progress on an executable phase. A failed
// phase triggers rollback; the final validating phase completing marks done.
func (s *Service) ReportPhase(id, phase, state, detail string) (domain.Job, error) {
	j, err := s.Get(id)
	if err != nil {
		return j, err
	}
	if state == "failed" {
		s.setPhase(&j, phase, "failed", detail)
		j.State = domain.JobRollback
		j.Reason = "phase " + phase + " failed: " + detail
		return j, s.save(j)
	}
	s.setPhase(&j, phase, state, detail)
	if phase == domain.JobValidating && state == "ok" {
		j.State = domain.JobDone
		j.FinishedAt = time.Now().Unix()
	} else {
		j.State = phase
	}
	return j, s.save(j)
}

// CompleteRollback finalizes a rollback outcome.
func (s *Service) CompleteRollback(id string, recovered bool) (domain.Job, error) {
	j, err := s.Get(id)
	if err != nil {
		return j, err
	}
	if recovered {
		j.State = domain.JobFailedRolledBack
	} else {
		j.State = domain.JobFailedUnrecovered
	}
	j.FinishedAt = time.Now().Unix()
	return j, s.save(j)
}

func (s *Service) fail(j *domain.Job, err error) (domain.Job, error) {
	j.State = domain.JobFailedUnrecovered
	j.Reason = err.Error()
	j.FinishedAt = time.Now().Unix()
	return *j, s.save(*j)
}

func (s *Service) setPhase(j *domain.Job, name, state, detail string) {
	now := time.Now().Unix()
	for i := range j.Phases {
		if j.Phases[i].Name == name {
			j.Phases[i].State = state
			j.Phases[i].Detail = detail
			if state == "running" && j.Phases[i].StartedAt == 0 {
				j.Phases[i].StartedAt = now
			}
			if state == "ok" || state == "failed" {
				j.Phases[i].FinishedAt = now
			}
			return
		}
	}
}

func isTerminal(state string) bool {
	switch state {
	case domain.JobDone, domain.JobFailedRolledBack, domain.JobFailedUnrecovered, domain.JobSuspended:
		return true
	}
	return false
}

// IsTerminal reports whether a job state is terminal (no further transitions).
func IsTerminal(state string) bool { return isTerminal(state) }

// ErrNotFound aliases the catalog error for API mapping.
var ErrNotFound = catalog.ErrNotFound
