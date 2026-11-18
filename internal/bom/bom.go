// Package bom implements desired-state bundles and their pre-publish gates
// (FR-3.2 / FR-3.8 / NFR-R7).
package bom

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// Service manages BOMs.
type Service struct {
	db  *sql.DB
	cat *catalog.Service
}

// New creates a BOM service.
func New(s *store.Store, cat *catalog.Service) *Service {
	return &Service{db: s.DB, cat: cat}
}

// Report is the outcome of validating a BOM.
type Report struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// OK reports whether the BOM may be published.
func (r Report) OK() bool { return len(r.Errors) == 0 }

// Create stores a new draft BOM, auto-incrementing the version per bom_id.
func (s *Service) Create(b domain.Bom) (domain.Bom, error) {
	if b.BomID == "" {
		return b, fmt.Errorf("bom_id is required")
	}
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(version) FROM boms WHERE bom_id=?`, b.BomID).Scan(&max); err != nil {
		return b, err
	}
	b.Version = int(max.Int64) + 1
	b.Status = domain.BomDraft
	now := time.Now().Unix()
	b.CreatedAt, b.UpdatedAt = now, now
	if err := s.insert(b); err != nil {
		return b, err
	}
	return b, nil
}

func (s *Service) insert(b domain.Bom) error {
	_, err := s.db.Exec(`INSERT INTO boms
		(bom_id,version,role,apps,dep_closure,config_templates,variables,units_enabled,aux_actions,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.BomID, b.Version, b.Role, store.ToJSON(b.Apps), store.ToJSON(b.DepClosure),
		store.ToJSON(b.ConfigTemplates), store.ToJSON(b.Variables), store.ToJSON(b.UnitsEnabled),
		store.ToJSON(b.AuxActions), b.Status, b.CreatedAt, b.UpdatedAt)
	return err
}

const bomCols = `bom_id,version,COALESCE(role,''),COALESCE(apps,'[]'),COALESCE(dep_closure,'[]'),
	COALESCE(config_templates,'{}'),COALESCE(variables,'{}'),COALESCE(units_enabled,'[]'),
	COALESCE(aux_actions,'[]'),status,created_at,updated_at`

func scanBom(row interface{ Scan(...any) error }) (domain.Bom, error) {
	var b domain.Bom
	var apps, dep, tpl, vars, units, aux string
	err := row.Scan(&b.BomID, &b.Version, &b.Role, &apps, &dep, &tpl, &vars, &units, &aux,
		&b.Status, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return b, err
	}
	store.FromJSON(apps, &b.Apps)
	store.FromJSON(dep, &b.DepClosure)
	store.FromJSON(tpl, &b.ConfigTemplates)
	store.FromJSON(vars, &b.Variables)
	store.FromJSON(units, &b.UnitsEnabled)
	store.FromJSON(aux, &b.AuxActions)
	return b, nil
}

// Get returns a specific BOM version.
func (s *Service) Get(bomID string, version int) (domain.Bom, error) {
	row := s.db.QueryRow(`SELECT `+bomCols+` FROM boms WHERE bom_id=? AND version=?`, bomID, version)
	b, err := scanBom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, catalog.ErrNotFound
	}
	return b, err
}

// Latest returns the highest version of a BOM.
func (s *Service) Latest(bomID string) (domain.Bom, error) {
	row := s.db.QueryRow(`SELECT `+bomCols+` FROM boms WHERE bom_id=? ORDER BY version DESC LIMIT 1`, bomID)
	b, err := scanBom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, catalog.ErrNotFound
	}
	return b, err
}

// List returns the latest version of every BOM.
func (s *Service) List() ([]domain.Bom, error) {
	rows, err := s.db.Query(`SELECT ` + bomCols + ` FROM boms b
		WHERE version=(SELECT MAX(version) FROM boms WHERE bom_id=b.bom_id)
		ORDER BY bom_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Bom{}
	for rows.Next() {
		b, err := scanBom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListPublished returns the latest published version of every BOM. A newer
// draft must never hide the published version that the fleet is bound to.
func (s *Service) ListPublished() ([]domain.Bom, error) {
	rows, err := s.db.Query(`SELECT ` + bomCols + ` FROM boms b
		WHERE status='published' AND version=(SELECT MAX(version) FROM boms
			WHERE bom_id=b.bom_id AND status='published')
		ORDER BY bom_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Bom{}
	for rows.Next() {
		b, err := scanBom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Versions returns every stored version of a BOM, newest first, so operators
// can inspect history and roll back to a version that is no longer the latest.
func (s *Service) Versions(bomID string) ([]domain.Bom, error) {
	rows, err := s.db.Query(`SELECT `+bomCols+` FROM boms WHERE bom_id=? ORDER BY version DESC`, bomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Bom{}
	for rows.Next() {
		b, err := scanBom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Import stores an exported BOM as a new draft version under the same bom_id.
func (s *Service) Import(b domain.Bom) (domain.Bom, error) {
	return s.Create(b)
}

// Rollback copies an existing version into a new PUBLISHED version so the
// fleet converges back to a known-good desired state.
func (s *Service) Rollback(bomID string, version int) (domain.Bom, error) {
	src, err := s.Get(bomID, version)
	if err != nil {
		return domain.Bom{}, err
	}
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(version) FROM boms WHERE bom_id=?`, bomID).Scan(&max); err != nil {
		return domain.Bom{}, err
	}
	src.Version = int(max.Int64) + 1
	src.Status = domain.BomPublished
	now := time.Now().Unix()
	src.CreatedAt, src.UpdatedAt = now, now
	if err := s.insert(src); err != nil {
		return domain.Bom{}, err
	}
	return src, nil
}

// Validate checks a BOM against the catalog and conflict rules. When arch is
// non-empty, artifact existence is checked for that exact architecture.
func (s *Service) Validate(b domain.Bom, arch string) Report {
	r := Report{Errors: []string{}, Warnings: []string{}}
	type resolved struct {
		app domain.App
		ver domain.AppVersion
	}
	var vers []resolved
	seenPorts := map[string]string{}
	dacOccupants := []string{}

	for _, ba := range b.Apps {
		app, err := s.cat.GetApp(ba.AppID)
		if err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("app %q not in catalog", ba.AppID))
			continue
		}
		if ba.Version == "" {
			r.Errors = append(r.Errors, fmt.Sprintf("app %q: explicit version required for reproducibility", ba.AppID))
			continue
		}
		var v domain.AppVersion
		if arch != "" {
			v, err = s.cat.ResolveVersion(ba.AppID, ba.Version, arch)
			if err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("NFR-R7: artifact %s %s (%s) missing from repo", ba.AppID, ba.Version, arch))
				continue
			}
		} else {
			v, err = s.findAnyArch(ba.AppID, ba.Version)
			if err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("NFR-R7: no artifact for %s %s", ba.AppID, ba.Version))
				continue
			}
		}
		// format=apt installs from the distro repository, whose own signature
		// chain replaces the artifact signature (NFR-S2 does not apply).
		if !v.Signed && v.Format != "apt" {
			r.Errors = append(r.Errors, fmt.Sprintf("NFR-S2: %s %s is unsigned", ba.AppID, ba.Version))
		}
		if v.Banned {
			r.Errors = append(r.Errors, fmt.Sprintf("%s %s is banned", ba.AppID, ba.Version))
		}
		if v.Lifecycle == domain.LifecycleRevoked {
			r.Errors = append(r.Errors, fmt.Sprintf("%s %s is revoked", ba.AppID, ba.Version))
		}
		// Capability consistency (AC-38): declared AirPlay2 must match build.
		if hasCap(v.Capabilities, "AirPlay2") && !v.AirPlay2Enabled {
			r.Errors = append(r.Errors, fmt.Sprintf("AC-38: %s %s declares AirPlay2 but build lacks it", ba.AppID, ba.Version))
		}
		for _, p := range v.Ports {
			key := fmt.Sprintf("%s/%d", p.Proto, p.Port)
			if other, ok := seenPorts[key]; ok && !p.Configurable {
				r.Errors = append(r.Errors, fmt.Sprintf("port conflict %s: %s vs %s", key, other, ba.AppID))
			}
			seenPorts[key] = ba.AppID
		}
		if v.OccupiesDAC {
			dacOccupants = append(dacOccupants, ba.AppID)
		}
		vers = append(vers, resolved{app, v})
	}

	// AirPlay 2 requires nqptp in the same BOM (FR-3.8.2).
	needsNqptp := false
	hasNqptp := false
	for _, rv := range vers {
		if rv.ver.AirPlay2Enabled {
			needsNqptp = true
		}
		if rv.app.AppID == "nqptp" {
			hasNqptp = true
		}
	}
	if needsNqptp && !hasNqptp {
		r.Errors = append(r.Errors, "AirPlay2 build present but nqptp missing from BOM (FR-3.8.2)")
	}

	// Multiple DAC occupants require an explicit arbitration policy (FR-3.8.4).
	if len(dacOccupants) > 1 && b.Variables["dac_arbitration"] == "" {
		r.Errors = append(r.Errors, fmt.Sprintf("multiple DAC occupants %v: set variables.dac_arbitration (manual|auto) to confirm", dacOccupants))
	}
	if len(dacOccupants) > 1 {
		r.Warnings = append(r.Warnings, "multiple renderers share one DAC; only one can play at a time")
	}
	return r
}

func (s *Service) findAnyArch(appID, version string) (domain.AppVersion, error) {
	all, err := s.cat.ListVersions(appID)
	if err != nil {
		return domain.AppVersion{}, err
	}
	for _, v := range all {
		if v.Version == version {
			return v, nil
		}
	}
	return domain.AppVersion{}, catalog.ErrNotFound
}

// Publish validates then marks a BOM published. It refuses to publish when the
// repo is not self-consistent (NFR-R7 / AC-36).
func (s *Service) Publish(bomID string, version int, arch string) (Report, error) {
	b, err := s.Get(bomID, version)
	if err != nil {
		return Report{}, err
	}
	r := s.Validate(b, arch)
	if !r.OK() {
		return r, fmt.Errorf("BOM validation failed: %d error(s)", len(r.Errors))
	}
	_, err = s.db.Exec(`UPDATE boms SET status=?, updated_at=? WHERE bom_id=? AND version=?`,
		domain.BomPublished, time.Now().Unix(), bomID, version)
	return r, err
}

func hasCap(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
