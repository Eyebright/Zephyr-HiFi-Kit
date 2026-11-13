// Package commands implements the server->agent command channel and the stored
// OS-update / network-diagnostic state reported back by agents (FR-6).
package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// Service queues commands and stores their results.
type Service struct{ db *sql.DB }

// New creates a commands service.
func New(s *store.Store) *Service { return &Service{db: s.DB} }

// ErrPlaying blocks an OS upgrade while the terminal is playing with play
// protection enabled (FR-3.10). The message is user-facing Chinese.
var ErrPlaying = errors.New("FR-3.10：终端正在播放且开启了播放保护，已拒绝升级")

// Create queues a pending command for a node after validating its type.
func (s *Service) Create(nodeID, typ string, params domain.CommandParams) (domain.Command, error) {
	var c domain.Command
	switch typ {
	case domain.CommandOSCheck, domain.CommandOSUpgrade, domain.CommandNetdiag:
	default:
		return c, fmt.Errorf("无效的命令类型：%s", typ)
	}
	var playing, protect int
	err := s.db.QueryRow(`SELECT playing,play_protect FROM nodes WHERE node_id=?`, nodeID).Scan(&playing, &protect)
	if errors.Is(err, sql.ErrNoRows) {
		return c, catalog.ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if typ == domain.CommandOSUpgrade && playing == 1 && protect == 1 {
		return c, ErrPlaying
	}
	c = domain.Command{
		ID:        "cmd-" + uuid.NewString(),
		NodeID:    nodeID,
		Type:      typ,
		Params:    params,
		Status:    "pending",
		CreatedAt: time.Now().Unix(),
	}
	_, err = s.db.Exec(`INSERT INTO commands(id,node_id,type,params,status,created_at)
		VALUES(?,?,?,?,?,?)`, c.ID, c.NodeID, c.Type, store.ToJSON(c.Params), c.Status, c.CreatedAt)
	return c, err
}

const cmdCols = `id,node_id,type,COALESCE(params,'{}'),status,created_at,
	COALESCE(finished_at,0),COALESCE(result_status,''),COALESCE(log,''),COALESCE(data,'')`

func scanCmd(row interface{ Scan(...any) error }) (domain.Command, error) {
	var c domain.Command
	var params, data string
	err := row.Scan(&c.ID, &c.NodeID, &c.Type, &params, &c.Status, &c.CreatedAt,
		&c.FinishedAt, &c.ResultStatus, &c.Log, &data)
	if err != nil {
		return c, err
	}
	store.FromJSON(params, &c.Params)
	if data != "" {
		c.Data = json.RawMessage(data)
	}
	return c, nil
}

// Get returns one command.
func (s *Service) Get(id string) (domain.Command, error) {
	row := s.db.QueryRow(`SELECT `+cmdCols+` FROM commands WHERE id=?`, id)
	c, err := scanCmd(row)
	if errors.Is(err, sql.ErrNoRows) {
		return c, catalog.ErrNotFound
	}
	return c, err
}

// List returns commands for a node. Pending commands are oldest-first (agent
// execution order); all others are newest-first (UI polling).
func (s *Service) List(nodeID, status string) ([]domain.Command, error) {
	q := `SELECT ` + cmdCols + ` FROM commands WHERE node_id=?`
	args := []any{nodeID}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	if status == "pending" {
		q += ` ORDER BY created_at ASC, rowid ASC`
	} else {
		q += ` ORDER BY created_at DESC, rowid DESC`
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Command{}
	for rows.Next() {
		c, err := scanCmd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Result marks a command done and stores the reported OS-update / netdiag state.
func (s *Service) Result(id, status, log string, data json.RawMessage) (domain.Command, error) {
	var c domain.Command
	if status != "ok" && status != "err" {
		return c, fmt.Errorf("无效的结果状态：%s", status)
	}
	c, err := s.Get(id)
	if err != nil {
		return c, err
	}
	if c.Status == "done" {
		return c, fmt.Errorf("命令已完成，不能重复上报")
	}
	now := time.Now().Unix()
	dataStr := ""
	if len(data) > 0 {
		dataStr = string(data)
	}
	if _, err := s.db.Exec(`UPDATE commands SET status='done',finished_at=?,result_status=?,log=?,data=? WHERE id=?`,
		now, status, log, dataStr, id); err != nil {
		return c, err
	}
	if status == "ok" {
		switch c.Type {
		case domain.CommandOSCheck, domain.CommandOSUpgrade:
			if err := s.storeOSUpdates(c.NodeID, data, now); err != nil {
				return c, err
			}
		case domain.CommandNetdiag:
			if err := s.storeNetDiag(c.NodeID, data, now); err != nil {
				return c, err
			}
		}
	}
	return s.Get(id)
}

func (s *Service) storeOSUpdates(nodeID string, data json.RawMessage, now int64) error {
	st := domain.OSUpdateState{Packages: []domain.OSPackage{}}
	if len(data) > 0 {
		store.FromJSON(string(data), &st)
	}
	if st.LastCheck == 0 {
		st.LastCheck = now
	}
	if st.Packages == nil {
		st.Packages = []domain.OSPackage{}
	}
	_, err := s.db.Exec(`INSERT INTO osupdates(node_id,state,updated_at) VALUES(?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at`,
		nodeID, store.ToJSON(st), now)
	return err
}

func (s *Service) storeNetDiag(nodeID string, data json.RawMessage, now int64) error {
	st := domain.NetDiagState{Results: []domain.NetdiagResult{}}
	if len(data) > 0 {
		store.FromJSON(string(data), &st)
	}
	if st.At == 0 {
		st.At = now
	}
	if st.Results == nil {
		st.Results = []domain.NetdiagResult{}
	}
	_, err := s.db.Exec(`INSERT INTO netdiag(node_id,state,updated_at) VALUES(?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at`,
		nodeID, store.ToJSON(st), now)
	return err
}

// OSUpdates returns the stored OS-update state, or an empty default.
func (s *Service) OSUpdates(nodeID string) (domain.OSUpdateState, error) {
	st := domain.OSUpdateState{Packages: []domain.OSPackage{}}
	var raw string
	err := s.db.QueryRow(`SELECT state FROM osupdates WHERE node_id=?`, nodeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	store.FromJSON(raw, &st)
	if st.Packages == nil {
		st.Packages = []domain.OSPackage{}
	}
	return st, nil
}

// NetDiag returns the stored network-diagnostic state, or an empty default.
func (s *Service) NetDiag(nodeID string) (domain.NetDiagState, error) {
	st := domain.NetDiagState{Results: []domain.NetdiagResult{}}
	var raw string
	err := s.db.QueryRow(`SELECT state FROM netdiag WHERE node_id=?`, nodeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	store.FromJSON(raw, &st)
	if st.Results == nil {
		st.Results = []domain.NetdiagResult{}
	}
	return st, nil
}
