// Package aptsrc manages third-party APT repositories delivered to terminals.
// Open-source audio services install from distro APT; repositories outside the
// distro (e.g. upmpdcli) are registered here and applied per node codename.
package aptsrc

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// Service manages the APT source registry.
type Service struct{ db *sql.DB }

// New creates an APT source service.
func New(s *store.Store) *Service { return &Service{db: s.DB} }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Codename extracts the Debian codename from a reported distro string such as
// "Debian GNU/Linux 12 (bookworm)". Empty when the report carries no codename.
func Codename(distro string) string {
	i := strings.LastIndex(distro, "(")
	j := strings.LastIndex(distro, ")")
	if i < 0 || j <= i+1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(distro[i+1 : j]))
}

// List returns all registered sources, oldest first.
func (s *Service) List() ([]domain.AptSource, error) {
	rows, err := s.db.Query(`SELECT id,name,url,components,key_url,distros,created_at
		FROM apt_sources ORDER BY created_at ASC, rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AptSource{}
	for rows.Next() {
		var a domain.AptSource
		var distros string
		if err := rows.Scan(&a.ID, &a.Name, &a.URL, &a.Components, &a.KeyURL, &distros, &a.CreatedAt); err != nil {
			return nil, err
		}
		store.FromJSON(distros, &a.Distros)
		if a.Distros == nil {
			a.Distros = []string{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Create validates and registers a source.
func (s *Service) Create(a domain.AptSource) (domain.AptSource, error) {
	a.Name = strings.TrimSpace(a.Name)
	a.URL = strings.TrimSpace(a.URL)
	a.KeyURL = strings.TrimSpace(a.KeyURL)
	a.Components = strings.TrimSpace(a.Components)
	if a.Name == "" {
		return a, fmt.Errorf("源名称不能为空")
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return a, fmt.Errorf("仓库 URL 必须以 http:// 或 https:// 开头")
	}
	if a.KeyURL != "" && !strings.HasPrefix(a.KeyURL, "http://") && !strings.HasPrefix(a.KeyURL, "https://") {
		return a, fmt.Errorf("密钥 URL 必须以 http:// 或 https:// 开头")
	}
	if a.Components == "" {
		a.Components = "main"
	}
	for _, f := range []string{a.URL, a.KeyURL, a.Components} {
		if strings.ContainsAny(f, " \t\r\n") {
			return a, fmt.Errorf("仓库字段不能包含空白字符")
		}
	}
	if a.ID == "" {
		a.ID = "src-" + uuid.NewString()
	}
	if !slugRe.MatchString(a.ID) {
		return a, fmt.Errorf("源 ID 只能包含小写字母、数字与连字符")
	}
	if a.Distros == nil {
		a.Distros = []string{}
	}
	for i, d := range a.Distros {
		a.Distros[i] = strings.ToLower(strings.TrimSpace(d))
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO apt_sources(id,name,url,components,key_url,distros,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.URL, a.Components, a.KeyURL, store.ToJSON(a.Distros), a.CreatedAt)
	if err != nil {
		return a, fmt.Errorf("insert apt source: %w", err)
	}
	return a, nil
}

// Delete removes a source from the registry (terminals keep their applied
// files until the source is re-applied or removed manually).
func (s *Service) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM apt_sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return catalog.ErrNotFound
	}
	return nil
}

// SpecsFor builds the command payload for one terminal: sources whose distro
// list matches the node codename (empty list = all). Suite stays empty so the
// agent resolves its own codename at apply time.
func (s *Service) SpecsFor(distro string) ([]domain.AptSourceSpec, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	code := Codename(distro)
	out := []domain.AptSourceSpec{}
	for _, a := range all {
		if code != "" && len(a.Distros) > 0 && !contains(a.Distros, code) {
			continue
		}
		out = append(out, domain.AptSourceSpec{
			ID: a.ID, Name: a.Name, URL: a.URL, Components: a.Components, KeyURL: a.KeyURL,
		})
	}
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Seed registers the upmpdcli upstream repository once. Debian-native packages
// (mpd, shairport-sync) need no extra source.
func (s *Service) Seed() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='seed_apt_sources_v1'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := s.Create(domain.AptSource{
		ID:         "upmpdcli",
		Name:       "upmpdcli 上游仓库",
		URL:        "http://www.lesbonscomptes.com/upmpdcli/downloads/raspbian/",
		Components: "main",
		KeyURL:     "https://www.lesbonscomptes.com/pages/lesbonscomptes.gpg",
		Distros:    []string{"bullseye", "bookworm", "trixie"},
	}); err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO server_state(key,value) VALUES('seed_apt_sources_v1','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
}