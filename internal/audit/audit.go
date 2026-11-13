// Package audit records an append-only change log for every mutation (FR-7.2).
package audit

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// Service writes and reads audit rows.
type Service struct{ db *sql.DB }

// New creates an audit service.
func New(s *store.Store) *Service { return &Service{db: s.DB} }

// Write appends one audit record. Secret material must never be placed in Detail.
func (s *Service) Write(e domain.Audit) error {
	if e.ID == "" {
		e.ID = "aud-" + uuid.NewString()
	}
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO audit
		(id,actor,ip,action,target,params,detail,result,reason,duration_ms,at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Actor, e.IP, e.Action, e.Target, "", e.Detail, e.Result, "", e.DurationMS, e.At)
	return err
}

const auditCols = `id,COALESCE(actor,''),COALESCE(ip,''),action,COALESCE(target,''),
	COALESCE(detail,''),COALESCE(result,''),COALESCE(duration_ms,0),at`

func scanAudit(row interface{ Scan(...any) error }) (domain.Audit, error) {
	var e domain.Audit
	err := row.Scan(&e.ID, &e.Actor, &e.IP, &e.Action, &e.Target, &e.Detail, &e.Result, &e.DurationMS, &e.At)
	return e, err
}

func (s *Service) query(limit int) ([]domain.Audit, error) {
	q := `SELECT ` + auditCols + ` FROM audit ORDER BY at DESC, rowid DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Audit{}
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// List returns the newest audit records (default 200 when limit <= 0).
func (s *Service) List(limit int) ([]domain.Audit, error) {
	if limit <= 0 {
		limit = 200
	}
	return s.query(limit)
}

// All returns every audit record, newest first (for export).
func (s *Service) All() ([]domain.Audit, error) { return s.query(0) }
