// Package rollouts implements staged, ring-based distribution (FR-3.5): nodes
// are assigned to canary rings by percent and each ring advances only after all
// of its jobs reach a terminal state.
package rollouts

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
)

// ErrRingBusy is returned when advancing while the current ring still has
// non-terminal jobs.
var ErrRingBusy = errors.New("当前环仍有未完成任务，请等待任务结束后再推进")

// RingSpec is one requested ring in a rollout creation payload.
type RingSpec struct {
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// Service manages rollouts.
type Service struct {
	db    *sql.DB
	nodes *node.Service
	tasks *task.Service
	boms  *bom.Service
}

// New creates a rollouts service.
func New(s *store.Store, n *node.Service, t *task.Service, b *bom.Service) *Service {
	return &Service{db: s.DB, nodes: n, tasks: t, boms: b}
}

// Create validates the plan, assigns online+authorized nodes round-robin by
// percent into rings and starts the first ring's jobs (gates apply).
func (s *Service) Create(name, bomID string, bomVersion int, specs []RingSpec) (domain.Rollout, error) {
	var ro domain.Rollout
	if strings.TrimSpace(name) == "" {
		return ro, fmt.Errorf("灰度名称不能为空")
	}
	if len(specs) == 0 {
		return ro, fmt.Errorf("至少需要一个灰度环")
	}
	sum := 0
	for _, sp := range specs {
		if sp.Percent <= 0 || sp.Percent > 100 {
			return ro, fmt.Errorf("灰度环百分比必须在 1..100 之间")
		}
		sum += sp.Percent
	}
	if sum != 100 {
		return ro, fmt.Errorf("各灰度环百分比之和必须为 100（当前 %d）", sum)
	}

	var b domain.Bom
	var err error
	if bomVersion > 0 {
		b, err = s.boms.Get(bomID, bomVersion)
	} else {
		b, err = s.boms.Latest(bomID)
	}
	if err != nil {
		return ro, fmt.Errorf("BOM 不存在：%w", err)
	}
	if b.Status != domain.BomPublished {
		return ro, fmt.Errorf("BOM %s v%d 未发布，无法创建灰度", b.BomID, b.Version)
	}

	all, err := s.nodes.List()
	if err != nil {
		return ro, err
	}
	eligible := []domain.Node{}
	for _, n := range all {
		if n.Status == domain.NodeOnline && n.Authorized {
			eligible = append(eligible, n)
		}
	}
	if len(eligible) == 0 {
		return ro, fmt.Errorf("没有在线且已授权的终端可参与灰度")
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].NodeID < eligible[j].NodeID })

	counts := allocate(len(eligible), specs)
	rings := make([]domain.RolloutRing, len(specs))
	for i, sp := range specs {
		rings[i] = domain.RolloutRing{Name: sp.Name, Percent: sp.Percent, Nodes: []string{}, Status: "pending"}
	}
	// Weighted round-robin: cycle rings, placing one node each while quota lasts.
	remaining := append([]int{}, counts...)
	idx, placed := 0, 0
	for placed < len(eligible) {
		for i := range rings {
			if remaining[i] > 0 && idx < len(eligible) {
				rings[i].Nodes = append(rings[i].Nodes, eligible[idx].NodeID)
				remaining[i]--
				idx++
				placed++
			}
		}
	}

	ro = domain.Rollout{
		ID:          "ro-" + uuid.NewString(),
		Name:        strings.TrimSpace(name),
		BomID:       b.BomID,
		BomVersion:  b.Version,
		Status:      domain.RolloutPending,
		CurrentRing: 0,
		CreatedAt:   time.Now().Unix(),
		Rings:       rings,
	}
	if err := s.startRing(&ro, 0); err != nil {
		return ro, err
	}
	if _, err := s.db.Exec(`INSERT INTO rollouts
		(id,name,bom_id,bom_version,status,current_ring,created_at,rings)
		VALUES(?,?,?,?,?,?,?,?)`,
		ro.ID, ro.Name, ro.BomID, ro.BomVersion, ro.Status, ro.CurrentRing, ro.CreatedAt,
		store.ToJSON(ro.Rings)); err != nil {
		return ro, err
	}
	return ro, nil
}

// startRing creates jobs for a ring's nodes via the existing job creator, so
// license/authorization/publish gates all apply.
func (s *Service) startRing(ro *domain.Rollout, i int) error {
	ring := &ro.Rings[i]
	ring.Jobs = []string{}
	for _, nid := range ring.Nodes {
		j, err := s.tasks.CreateJob(nid, ro.BomID, ro.BomVersion, domain.KindUpgrade)
		if err != nil {
			return fmt.Errorf("终端 %s 任务创建失败：%w", nid, err)
		}
		ring.Jobs = append(ring.Jobs, j.ID)
	}
	if len(ring.Nodes) > 0 {
		ring.Status = "running"
	}
	return nil
}

const rolloutCols = `id,COALESCE(name,''),COALESCE(bom_id,''),COALESCE(bom_version,0),
	status,COALESCE(current_ring,0),created_at,COALESCE(rings,'[]')`

func scanRollout(row interface{ Scan(...any) error }) (domain.Rollout, error) {
	var ro domain.Rollout
	var rings string
	err := row.Scan(&ro.ID, &ro.Name, &ro.BomID, &ro.BomVersion, &ro.Status,
		&ro.CurrentRing, &ro.CreatedAt, &rings)
	if err != nil {
		return ro, err
	}
	store.FromJSON(rings, &ro.Rings)
	if ro.Rings == nil {
		ro.Rings = []domain.RolloutRing{}
	}
	return ro, nil
}

// Get returns one rollout.
func (s *Service) Get(id string) (domain.Rollout, error) {
	row := s.db.QueryRow(`SELECT `+rolloutCols+` FROM rollouts WHERE id=?`, id)
	ro, err := scanRollout(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ro, catalog.ErrNotFound
	}
	return ro, err
}

// List returns rollouts newest first.
func (s *Service) List() ([]domain.Rollout, error) {
	rows, err := s.db.Query(`SELECT ` + rolloutCols + ` FROM rollouts ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Rollout{}
	for rows.Next() {
		ro, err := scanRollout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

func (s *Service) save(ro domain.Rollout) error {
	_, err := s.db.Exec(`UPDATE rollouts SET status=?,current_ring=?,rings=? WHERE id=?`,
		ro.Status, ro.CurrentRing, store.ToJSON(ro.Rings), ro.ID)
	return err
}

// Advance completes the current ring when all its jobs are terminal, then
// starts the next ring. The last ring finishing marks the rollout done.
func (s *Service) Advance(id string) (domain.Rollout, error) {
	ro, err := s.Get(id)
	if err != nil {
		return ro, err
	}
	if ro.Status == domain.RolloutDone {
		return ro, nil
	}
	if ro.CurrentRing < 0 || ro.CurrentRing >= len(ro.Rings) {
		return ro, fmt.Errorf("灰度环索引无效")
	}
	ring := &ro.Rings[ro.CurrentRing]
	for _, jid := range ring.Jobs {
		j, err := s.tasks.Get(jid)
		if err != nil {
			return ro, err
		}
		if !task.IsTerminal(j.State) {
			return ro, ErrRingBusy
		}
	}
	ring.Status = "done"
	if ro.CurrentRing == len(ro.Rings)-1 {
		ro.Status = domain.RolloutDone
	} else {
		ro.CurrentRing++
		if err := s.startRing(&ro, ro.CurrentRing); err != nil {
			return ro, err
		}
		ro.Status = domain.RolloutRunning
	}
	if err := s.save(ro); err != nil {
		return ro, err
	}
	return ro, nil
}

// Delete removes a rollout (its jobs are retained for history).
func (s *Service) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM rollouts WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return catalog.ErrNotFound
	}
	return nil
}

// StripNode removes a deleted node from every rollout's rings.
func (s *Service) StripNode(nodeID string) error {
	all, err := s.List()
	if err != nil {
		return err
	}
	for _, ro := range all {
		changed := false
		for i := range ro.Rings {
			kept := ro.Rings[i].Nodes[:0]
			for _, nid := range ro.Rings[i].Nodes {
				if nid != nodeID {
					kept = append(kept, nid)
				} else {
					changed = true
				}
			}
			ro.Rings[i].Nodes = kept
		}
		if changed {
			if err := s.save(ro); err != nil {
				return err
			}
		}
	}
	return nil
}

// allocate distributes n nodes across rings by percent using largest-remainder
// rounding so the quotas always sum to n.
func allocate(n int, specs []RingSpec) []int {
	counts := make([]int, len(specs))
	rem := make([]float64, len(specs))
	total := 0
	for i, sp := range specs {
		exact := float64(sp.Percent) * float64(n) / 100.0
		counts[i] = int(math.Floor(exact))
		rem[i] = exact - float64(counts[i])
		total += counts[i]
	}
	for total < n {
		best := -1
		for i := range specs {
			if rem[i] >= 0 && (best == -1 || rem[i] > rem[best]) {
				best = i
			}
		}
		if best == -1 {
			break
		}
		counts[best]++
		rem[best] = -1
		total++
	}
	return counts
}
