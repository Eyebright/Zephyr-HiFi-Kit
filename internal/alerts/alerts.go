// Package alerts implements deduplicated operational alerts and the rule scan
// (FR-7.1): offline nodes, pending reboots, license expiry and full seats.
package alerts

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/license"
	"hifihub/server/internal/node"
	"hifihub/server/internal/store"
)

// Alert kinds produced by Scan.
const (
	KindNodeOffline     = "node_offline"
	KindRebootRequired  = "reboot_required"
	KindLicenseExpired  = "license_expired"
	KindLicenseExpiring = "license_expiring"
	KindSeatsFull       = "seats_full"
)

// Service reads/writes the alerts table and scans alert rules.
type Service struct {
	db    *sql.DB
	nodes *node.Service
	lic   *license.Service
	cmds  *commands.Service
}

// New creates an alerts service.
func New(s *store.Store, n *node.Service, l *license.Service, c *commands.Service) *Service {
	return &Service{db: s.DB, nodes: n, lic: l, cmds: c}
}

const alertCols = `id,first_at,CASE WHEN COALESCE(kind,'')='' THEN rule ELSE kind END,
	severity,COALESCE(node_id,''),COALESCE(message,''),COALESCE(status,'open'),
	COALESCE(ack_at,0),COALESCE(resolved_at,0)`

func scanAlert(row interface{ Scan(...any) error }) (domain.Alert, error) {
	var a domain.Alert
	err := row.Scan(&a.ID, &a.At, &a.Kind, &a.Severity, &a.NodeID, &a.Message,
		&a.Status, &a.AckAt, &a.ResolvedAt)
	return a, err
}

// List returns alerts newest first, optionally filtered by status.
func (s *Service) List(status string) ([]domain.Alert, error) {
	q := `SELECT ` + alertCols + ` FROM alerts`
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY first_at DESC, rowid DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get returns one alert.
func (s *Service) Get(id string) (domain.Alert, error) {
	row := s.db.QueryRow(`SELECT `+alertCols+` FROM alerts WHERE id=?`, id)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return a, catalog.ErrNotFound
	}
	return a, err
}

// Ack acknowledges an open alert.
func (s *Service) Ack(id string) (domain.Alert, error) {
	a, err := s.Get(id)
	if err != nil {
		return a, err
	}
	if a.Status == domain.AlertResolved {
		return a, fmt.Errorf("告警已解决，无法确认")
	}
	if a.Status != domain.AlertAck {
		if _, err := s.db.Exec(`UPDATE alerts SET status=?,ack_at=? WHERE id=?`,
			domain.AlertAck, time.Now().Unix(), id); err != nil {
			return a, err
		}
	}
	return s.Get(id)
}

// Resolve marks an alert resolved.
func (s *Service) Resolve(id string) (domain.Alert, error) {
	a, err := s.Get(id)
	if err != nil {
		return a, err
	}
	if a.Status != domain.AlertResolved {
		if _, err := s.db.Exec(`UPDATE alerts SET status=?,resolved_at=? WHERE id=?`,
			domain.AlertResolved, time.Now().Unix(), id); err != nil {
			return a, err
		}
	}
	return s.Get(id)
}

// AckAll acknowledges every open alert, returning the affected count.
func (s *Service) AckAll() (int64, error) {
	res, err := s.db.Exec(`UPDATE alerts SET status=?,ack_at=? WHERE status=?`,
		domain.AlertAck, time.Now().Unix(), domain.AlertOpen)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Scan evaluates all alert rules and upserts deduplicated open alerts. It
// returns the number of active conditions found.
func (s *Service) Scan() (int, error) {
	now := time.Now().Unix()
	count := 0
	nodes, err := s.nodes.List()
	if err != nil {
		return 0, err
	}
	for _, n := range nodes {
		if n.LastSeen > 0 && now-n.LastSeen > int64(n.OfflineAfter) {
			if err := s.upsertOpen(KindNodeOffline, n.NodeID, domain.SeverityCritical,
				fmt.Sprintf("终端 %s 离线超过 %d 秒", n.Alias, n.OfflineAfter), now); err != nil {
				return count, err
			}
			count++
		}
		st, err := s.cmds.OSUpdates(n.NodeID)
		if err == nil && st.RebootRequired {
			if err := s.upsertOpen(KindRebootRequired, n.NodeID, domain.SeverityWarning,
				fmt.Sprintf("终端 %s 系统更新后需要重启", n.Alias), now); err != nil {
				return count, err
			}
			count++
		}
	}
	lic, err := s.lic.Get()
	if err != nil {
		return count, err
	}
	switch lic.Status {
	case domain.LicenseExpired:
		if err := s.upsertOpen(KindLicenseExpired, "", domain.SeverityCritical,
			"许可证已过期，请及时续期", now); err != nil {
			return count, err
		}
		count++
	case domain.LicenseActivated:
		if remain := lic.ExpiresAt - now; remain <= 7*86400 {
			days := remain / 86400
			if days < 0 {
				days = 0
			}
			if err := s.upsertOpen(KindLicenseExpiring, "", domain.SeverityWarning,
				fmt.Sprintf("许可证将在 %d 天内到期，请及时续期", days), now); err != nil {
				return count, err
			}
			count++
		}
		used, err := s.nodes.AuthorizedCount()
		if err != nil {
			return count, err
		}
		if used >= lic.Seats {
			if err := s.upsertOpen(KindSeatsFull, "", domain.SeverityWarning,
				fmt.Sprintf("许可证席位已满（%d/%d），新终端将无法授权", used, lic.Seats), now); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// upsertOpen refreshes an existing open alert of the same kind+node, or inserts
// a new one (dedup-open semantics).
func (s *Service) upsertOpen(kind, nodeID, severity, message string, now int64) error {
	var id string
	err := s.db.QueryRow(`SELECT id FROM alerts WHERE kind=? AND COALESCE(node_id,'')=? AND status=?`,
		kind, nodeID, domain.AlertOpen).Scan(&id)
	if err == nil {
		_, err = s.db.Exec(`UPDATE alerts SET last_at=?,count=count+1,message=?,severity=? WHERE id=?`,
			now, message, severity, id)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO alerts
		(id,rule,kind,severity,node_id,message,first_at,last_at,count,status,ack_at,resolved_at,acked_by)
		VALUES(?,?,?,?,?,?,?,?,1,?,0,0,'')`,
		"al-"+uuid.NewString(), kind, kind, severity, nodeID, message, now, now, domain.AlertOpen)
	return err
}
