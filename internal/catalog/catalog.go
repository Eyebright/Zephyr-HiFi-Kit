// Package catalog implements the application catalog (FR-3.1).
package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

// Service manages apps and their versions.
type Service struct {
	db *sql.DB
}

// New creates a catalog service.
func New(s *store.Store) *Service { return &Service{db: s.DB} }

// CreateApp registers a new application.
func (s *Service) CreateApp(a domain.App) (domain.App, error) {
	if a.AppID == "" {
		return a, fmt.Errorf("app_id is required")
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	if a.MaintainerState == "" {
		a.MaintainerState = "active"
	}
	_, err := s.db.Exec(`INSERT INTO apps
		(app_id,name,category,upstream,license,maintainer_state,self_build_required,default_channel,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		a.AppID, a.Name, string(a.Category), a.Upstream, a.License, a.MaintainerState,
		boolInt(a.SelfBuildRequired), a.DefaultChannel, a.CreatedAt)
	if err != nil {
		return a, fmt.Errorf("insert app: %w", err)
	}
	return a, nil
}

// GetApp returns one app by id.
func (s *Service) GetApp(id string) (domain.App, error) {
	var a domain.App
	var selfBuild int
	err := s.db.QueryRow(`SELECT app_id,name,category,COALESCE(upstream,''),COALESCE(license,''),
		COALESCE(maintainer_state,''),self_build_required,COALESCE(default_channel,''),created_at
		FROM apps WHERE app_id=?`, id).
		Scan(&a.AppID, &a.Name, &a.Category, &a.Upstream, &a.License, &a.MaintainerState,
			&selfBuild, &a.DefaultChannel, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.SelfBuildRequired = selfBuild == 1
	return a, nil
}

// ListApps returns all apps ordered by id.
func (s *Service) ListApps() ([]domain.App, error) {
	rows, err := s.db.Query(`SELECT app_id,name,category,COALESCE(upstream,''),COALESCE(license,''),
		COALESCE(maintainer_state,''),self_build_required,COALESCE(default_channel,''),created_at
		FROM apps ORDER BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.App{}
	for rows.Next() {
		var a domain.App
		var selfBuild int
		if err := rows.Scan(&a.AppID, &a.Name, &a.Category, &a.Upstream, &a.License,
			&a.MaintainerState, &selfBuild, &a.DefaultChannel, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.SelfBuildRequired = selfBuild == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddVersion registers a concrete artifact version (FR-3.1.3 / FR-3.11.9).
func (s *Service) AddVersion(v domain.AppVersion) (domain.AppVersion, error) {
	if v.AppID == "" || v.Version == "" || v.Arch == "" {
		return v, fmt.Errorf("app_id, version and arch are required")
	}
	if _, err := s.GetApp(v.AppID); err != nil {
		return v, fmt.Errorf("unknown app %q: %w", v.AppID, err)
	}
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	if v.Lifecycle == "" {
		v.Lifecycle = domain.LifecycleAvailable
	}
	if v.PublishedAt == 0 {
		v.PublishedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO app_versions
		(id,app_id,version,arch,format,artifact_path,size,sha256,signature,signed,build_flags,capabilities,
		 distro_support,glibc_min,units,ports,config_template_id,deps_apps,deps_pkgs,occupies_dac,
		 build_commit,airplay2_enabled,qplay_ver,patched,banned,lifecycle,build_time,published_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.AppID, v.Version, v.Arch, v.Format, v.ArtifactPath, v.Size, v.SHA256, v.Signature, boolInt(v.Signed),
		store.ToJSON(v.BuildFlags), store.ToJSON(v.Capabilities), store.ToJSON(v.DistroSupport),
		v.GlibcMin, store.ToJSON(v.Units), store.ToJSON(v.Ports), v.ConfigTemplateID,
		store.ToJSON(v.DepsApps), store.ToJSON(v.DepsPkgs), boolInt(v.OccupiesDAC), v.BuildCommit,
		boolInt(v.AirPlay2Enabled), v.QPlayVer, boolInt(v.Patched), boolInt(v.Banned), v.Lifecycle,
		v.BuildTime, v.PublishedAt)
	if err != nil {
		return v, fmt.Errorf("insert version: %w", err)
	}
	return v, nil
}

const versionCols = `id,app_id,version,arch,COALESCE(format,''),COALESCE(artifact_path,''),size,COALESCE(sha256,''),
	COALESCE(signature,''),signed,COALESCE(build_flags,'[]'),COALESCE(capabilities,'[]'),
	COALESCE(distro_support,'[]'),COALESCE(glibc_min,''),COALESCE(units,'[]'),COALESCE(ports,'[]'),
	COALESCE(config_template_id,''),COALESCE(deps_apps,'[]'),COALESCE(deps_pkgs,'[]'),occupies_dac,
	COALESCE(build_commit,''),airplay2_enabled,qplay_ver,patched,banned,lifecycle,
	COALESCE(build_time,0),COALESCE(published_at,0)`

func scanVersion(row interface{ Scan(...any) error }) (domain.AppVersion, error) {
	var v domain.AppVersion
	var signed, occupies, ap2, patched, banned int
	var flags, caps, distros, units, ports, depsApps, depsPkgs string
	err := row.Scan(&v.ID, &v.AppID, &v.Version, &v.Arch, &v.Format, &v.ArtifactPath, &v.Size, &v.SHA256,
		&v.Signature, &signed, &flags, &caps, &distros, &v.GlibcMin, &units, &ports,
		&v.ConfigTemplateID, &depsApps, &depsPkgs, &occupies, &v.BuildCommit, &ap2,
		&v.QPlayVer, &patched, &banned, &v.Lifecycle, &v.BuildTime, &v.PublishedAt)
	if err != nil {
		return v, err
	}
	v.Signed = signed == 1
	v.OccupiesDAC = occupies == 1
	v.AirPlay2Enabled = ap2 == 1
	v.Patched = patched == 1
	v.Banned = banned == 1
	store.FromJSON(flags, &v.BuildFlags)
	store.FromJSON(caps, &v.Capabilities)
	store.FromJSON(distros, &v.DistroSupport)
	store.FromJSON(units, &v.Units)
	store.FromJSON(ports, &v.Ports)
	store.FromJSON(depsApps, &v.DepsApps)
	store.FromJSON(depsPkgs, &v.DepsPkgs)
	return v, nil
}

// GetVersion returns a specific app/version/arch artifact.
func (s *Service) GetVersion(appID, version, arch string) (domain.AppVersion, error) {
	row := s.db.QueryRow(`SELECT `+versionCols+` FROM app_versions
		WHERE app_id=? AND version=? AND arch=?`, appID, version, arch)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// GetVersionByID returns a version by its internal id.
func (s *Service) GetVersionByID(id string) (domain.AppVersion, error) {
	row := s.db.QueryRow(`SELECT `+versionCols+` FROM app_versions WHERE id=?`, id)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// ListVersions returns all versions of an app.
func (s *Service) ListVersions(appID string) ([]domain.AppVersion, error) {
	rows, err := s.db.Query(`SELECT `+versionCols+` FROM app_versions WHERE app_id=?
		ORDER BY published_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AppVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetBanned marks a version banned/unbanned (FR-3.11.12). Files are retained.
func (s *Service) SetBanned(id string, banned bool) error {
	res, err := s.db.Exec(`UPDATE app_versions SET banned=? WHERE id=?`, boolInt(banned), id)
	return affected(res, err)
}

// SetLifecycle transitions a version's lifecycle (FR-3.1.6).
func (s *Service) SetLifecycle(id, lifecycle string) error {
	res, err := s.db.Exec(`UPDATE app_versions SET lifecycle=? WHERE id=?`, lifecycle, id)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}