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
	if !domain.ValidAppOrigin(a.Origin) {
		a.Origin = domain.OriginSelf
	}
	_, err := s.db.Exec(`INSERT INTO apps
		(app_id,name,category,origin,upstream,license,maintainer_state,self_build_required,default_channel,role,archs,notes,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.AppID, a.Name, string(a.Category), string(a.Origin), a.Upstream, a.License, a.MaintainerState,
		boolInt(a.SelfBuildRequired), a.DefaultChannel, a.Role, a.Archs, a.Notes, a.CreatedAt)
	if err != nil {
		return a, fmt.Errorf("insert app: %w", err)
	}
	return a, nil
}

// GetApp returns one app by id.
func (s *Service) GetApp(id string) (domain.App, error) {
	var a domain.App
	var selfBuild int
	var origin string
	err := s.db.QueryRow(`SELECT app_id,name,category,COALESCE(origin,'self'),COALESCE(upstream,''),COALESCE(license,''),
		COALESCE(maintainer_state,''),self_build_required,COALESCE(default_channel,''),COALESCE(role,''),COALESCE(archs,''),COALESCE(notes,''),created_at
		FROM apps WHERE app_id=?`, id).
		Scan(&a.AppID, &a.Name, &a.Category, &origin, &a.Upstream, &a.License, &a.MaintainerState,
			&selfBuild, &a.DefaultChannel, &a.Role, &a.Archs, &a.Notes, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.SelfBuildRequired = selfBuild == 1
	a.Origin = domain.AppOrigin(origin)
	if !domain.ValidAppOrigin(a.Origin) {
		a.Origin = domain.OriginSelf
	}
	return a, nil
}

// SetAppOrigin reclassifies how an app is obtained and returns the updated app.
func (s *Service) SetAppOrigin(id string, origin domain.AppOrigin) (domain.App, error) {
	if !domain.ValidAppOrigin(origin) {
		return domain.App{}, fmt.Errorf("unknown app origin %q", origin)
	}
	res, err := s.db.Exec(`UPDATE apps SET origin=? WHERE app_id=?`, string(origin), id)
	if err := affected(res, err); err != nil {
		return domain.App{}, err
	}
	return s.GetApp(id)
}

// ListApps returns all apps ordered by id.
func (s *Service) ListApps() ([]domain.App, error) {
	rows, err := s.db.Query(`SELECT app_id,name,category,COALESCE(origin,'self'),COALESCE(upstream,''),COALESCE(license,''),
		COALESCE(maintainer_state,''),self_build_required,COALESCE(default_channel,''),COALESCE(role,''),COALESCE(archs,''),COALESCE(notes,''),created_at
		FROM apps ORDER BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.App{}
	for rows.Next() {
		var a domain.App
		var selfBuild int
		var origin string
		if err := rows.Scan(&a.AppID, &a.Name, &a.Category, &origin, &a.Upstream, &a.License,
			&a.MaintainerState, &selfBuild, &a.DefaultChannel, &a.Role, &a.Archs, &a.Notes, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.SelfBuildRequired = selfBuild == 1
		a.Origin = domain.AppOrigin(origin)
		if !domain.ValidAppOrigin(a.Origin) {
			a.Origin = domain.OriginSelf
		}
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
		(id,app_id,version,arch,format,apt_package,artifact_path,size,sha256,signature,signed,build_flags,capabilities,
		 distro_support,glibc_min,units,ports,config_template_id,deps_apps,deps_pkgs,occupies_dac,
		 build_commit,airplay2_enabled,qplay_ver,patched,banned,lifecycle,build_time,published_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.AppID, v.Version, v.Arch, v.Format, v.AptPackage, v.ArtifactPath, v.Size, v.SHA256, v.Signature, boolInt(v.Signed),
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

const versionCols = `id,app_id,version,arch,COALESCE(format,''),COALESCE(apt_package,''),COALESCE(artifact_path,''),size,COALESCE(sha256,''),
	COALESCE(signature,''),signed,COALESCE(build_flags,'[]'),COALESCE(capabilities,'[]'),
	COALESCE(distro_support,'[]'),COALESCE(glibc_min,''),COALESCE(units,'[]'),COALESCE(ports,'[]'),
	COALESCE(config_template_id,''),COALESCE(deps_apps,'[]'),COALESCE(deps_pkgs,'[]'),occupies_dac,
	COALESCE(build_commit,''),airplay2_enabled,qplay_ver,patched,banned,lifecycle,
	COALESCE(build_time,0),COALESCE(published_at,0)`

func scanVersion(row interface{ Scan(...any) error }) (domain.AppVersion, error) {
	var v domain.AppVersion
	var signed, occupies, ap2, patched, banned int
	var flags, caps, distros, units, ports, depsApps, depsPkgs string
	err := row.Scan(&v.ID, &v.AppID, &v.Version, &v.Arch, &v.Format, &v.AptPackage, &v.ArtifactPath, &v.Size, &v.SHA256,
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

// ResolveVersion looks up a version for the node arch, falling back to the
// architecture-independent "all" entry used by format=apt packages.
func (s *Service) ResolveVersion(appID, version, arch string) (domain.AppVersion, error) {
	v, err := s.GetVersion(appID, version, arch)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrNotFound) || arch == "all" {
		return v, err
	}
	return s.GetVersion(appID, version, "all")
}

// AptApp pairs an open-source catalog app with its format=apt version.
type AptApp struct {
	App     domain.App        `json:"app"`
	Version domain.AppVersion `json:"version"`
}

// ListAptApps returns every catalog app that installs through distro APT.
func (s *Service) ListAptApps() ([]AptApp, error) {
	rows, err := s.db.Query(`SELECT ` + versionCols + ` FROM app_versions
		WHERE format='apt' AND banned=0 AND lifecycle<>'revoked' ORDER BY app_id`)
	if err != nil {
		return nil, err
	}
	vers := []domain.AppVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		vers = append(vers, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// The store serializes on one connection: app lookups must run after the
	// version rows are closed, never inside the iteration.
	out := []AptApp{}
	for _, v := range vers {
		app, err := s.GetApp(v.AppID)
		if err != nil {
			continue
		}
		out = append(out, AptApp{App: app, Version: v})
	}
	return out, nil
}

// SeedAptApps registers the open-source audio services once (idempotent via a
// server_state marker): they install from distro APT, not from signed artifacts.
func (s *Service) SeedAptApps() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='seed_apt_apps_v1'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	seed := []struct {
		app  domain.App
		pkg  string
		deps []string
		unit string
	}{
		{domain.App{AppID: "mpd", Name: "Music Player Daemon", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://www.musicpd.org", License: "GPL-2.0"}, "mpd", nil, "mpd.service"},
		{domain.App{AppID: "upmpdcli", Name: "UPnP Media Renderer (upmpdcli)", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://www.lesbonscomptes.com/upmpdcli/", License: "GPL-2.0"}, "upmpdcli", []string{"mpd"}, "upmpdcli.service"},
		{domain.App{AppID: "shairport-sync", Name: "AirPlay 1 接收器 (shairport-sync)", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://github.com/mikebrady/shairport-sync", License: "MIT"}, "shairport-sync", nil, "shairport-sync.service"},
	}
	for _, sd := range seed {
		if _, err := s.GetApp(sd.app.AppID); err != nil {
			if _, err := s.CreateApp(sd.app); err != nil {
				return err
			}
		}
		if _, err := s.GetVersion(sd.app.AppID, "apt", "all"); err != nil {
			v := domain.AppVersion{
				AppID: sd.app.AppID, Version: "apt", Arch: "all", Format: "apt",
				AptPackage: sd.pkg, DepsPkgs: sd.deps, Units: []string{sd.unit},
				DistroSupport: []string{"bookworm", "trixie", "bullseye"},
				Signed:        true, // distro repository signature, not artifact chain
			}
			if _, err := s.AddVersion(v); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`INSERT INTO server_state(key,value) VALUES('seed_apt_apps_v1','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
}

// SeedCommercialApps registers the commercial renderer app definitions once.
// Their versions carry vendor installers (tar.bz2 / deb) and are added through
// the local-package import endpoint, never seeded with empty artifacts.
func (s *Service) SeedCommercialApps() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='seed_commercial_apps_v1'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	seed := []domain.App{
		{AppID: "roonbridge", Name: "Roon Bridge", Category: domain.CategoryRenderer, Origin: domain.OriginSelf,
			Upstream: "https://roonlabs.com", License: "Commercial"},
		{AppID: "networkaudiod", Name: "HQPlayer NAA", Category: domain.CategoryRenderer, Origin: domain.OriginVendor,
			Upstream: "https://signalyst.com", License: "Commercial"},
	}
	for _, app := range seed {
		if _, err := s.GetApp(app.AppID); err != nil {
			if _, err := s.CreateApp(app); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`INSERT INTO server_state(key,value) VALUES('seed_commercial_apps_v1','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
}

// SeedComponentLibrary registers the reference component list (protocol
// endpoints and renderers) with role/architecture/install-strategy metadata.
// Existing entries keep their origin and versions; only descriptive metadata
// is refreshed so admin reclassifications survive re-seeding.
func (s *Service) SeedComponentLibrary() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='seed_component_lib_v1'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	seed := []struct {
		app  domain.App
		pkg  string
		unit string
	}{
		{domain.App{AppID: "roonbridge", Name: "Roon Bridge", Category: domain.CategoryRenderer, Origin: domain.OriginSelf,
			Upstream: "https://roonlabs.com", License: "Commercial",
			Role:  "RAAT 端点，把本机 DAC 暴露给 Roon Core",
			Archs: "x86_64 / arm64 / armhf（Win/Mac 不在托管范围）",
			Notes: "轻量只做输出，专为专用/半专用设备优化；经签名本地包导入安装"}, "", ""},
		{domain.App{AppID: "networkaudiod", Name: "HQPlayer NAA", Category: domain.CategoryRenderer, Origin: domain.OriginVendor,
			Upstream: "https://signalyst.com", License: "Commercial",
			Role:  "NAA 网络音频适配器，远端收 HQPlayer 流",
			Archs: "x86_64 / arm64 / armhf",
			Notes: "需另一台机器跑 HQPlayer 核心；经厂商签名本地包导入安装"}, "", ""},
		{domain.App{AppID: "shairport-sync", Name: "AirPlay 1 接收器 (shairport-sync)", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://github.com/mikebrady/shairport-sync", License: "MIT",
			Role:  "AirPlay 1 / AirPlay 2 接收端（RAOP）",
			Archs: "x86_64 / arm64 / armhf（Linux/BSD）",
			Notes: "经典无损 16/44.1 ALAC，新协议栈可做 AirPlay 2；发行版 APT 安装"}, "shairport-sync", "shairport-sync.service"},
		{domain.App{AppID: "gmediarender", Name: "gmrender-resurrect (gmediarender)", Category: domain.CategoryRenderer, Origin: domain.OriginSelf,
			Upstream: "https://github.com/hzeller/gmrender-resurrect", License: "GPL-2.0", SelfBuildRequired: true,
			Role:  "UPnP/DLNA 渲染端",
			Archs: "x86_64 / arm64 / armhf",
			Notes: "支持到 24/192 hi-res；bookworm 无现成包，需自备预编译 tar.bz2 或源码构建"}, "", ""},
		{domain.App{AppID: "librespot", Name: "librespot", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://github.com/librespot-org/librespot", License: "MIT",
			Role:  "Spotify Connect 接收端",
			Archs: "x86_64 / arm64 / armhf（Linux/BSD）",
			Notes: "开源，跑在 SBC/NAS 上即可被 Spotify App 发现；包由 moode 源提供，纯 Debian 需自备构建"}, "librespot", ""},
		{domain.App{AppID: "squeezelite", Name: "Squeezelite", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://github.com/ralph-irving/squeezelite", License: "GPL-3.0",
			Role:  "LMS/Squeezebox 软解端点",
			Archs: "x86_64 / arm64 / armhf",
			Notes: "LMS 生态的标准软解播放器；发行版 APT 安装"}, "squeezelite", "squeezelite.service"},
		{domain.App{AppID: "snapclient", Name: "Snapcast client", Category: domain.CategoryRenderer, Origin: domain.OriginDistro,
			Upstream: "https://github.com/badaix/snapcast", License: "GPL-3.0",
			Role:  "多房间同步渲染端",
			Archs: "x86_64 / arm64 / armhf（Linux/macOS/Win/Android，托管范围 Linux）",
			Notes: "与 snapserver 时间同步、组播同曲；发行版 APT 安装"}, "snapclient", "snapclient.service"},
		{domain.App{AppID: "qbzd", Name: "qbzd（Qobuz Connect 守护进程）", Category: domain.CategoryRenderer, Origin: domain.OriginSelf,
			Upstream: "https://www.qobuz.com", License: "Commercial",
			Role:  "Qobuz Connect 端点，bit-perfect",
			Archs: "x86_64 / arm64",
			Notes: "~25MB 单二进制；systemd/OpenRC/runit 服务、MPRIS、SSH 下浏览器登录；自备签名本地包导入"}, "", ""},
	}
	for _, sd := range seed {
		if _, err := s.GetApp(sd.app.AppID); err != nil {
			if _, err := s.CreateApp(sd.app); err != nil {
				return err
			}
		} else if _, err := s.db.Exec(`UPDATE apps SET role=?, archs=?, notes=? WHERE app_id=?`,
			sd.app.Role, sd.app.Archs, sd.app.Notes, sd.app.AppID); err != nil {
			return err
		}
		if sd.pkg == "" {
			continue
		}
		if _, err := s.GetVersion(sd.app.AppID, "apt", "all"); err != nil {
			v := domain.AppVersion{
				AppID: sd.app.AppID, Version: "apt", Arch: "all", Format: "apt",
				AptPackage: sd.pkg, Units: []string{sd.unit},
				DistroSupport: []string{"bookworm", "trixie", "bullseye"},
				Signed:        true, // distro repository signature, not artifact chain
			}
			if sd.unit == "" {
				v.Units = nil
			}
			if _, err := s.AddVersion(v); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`INSERT INTO server_state(key,value) VALUES('seed_component_lib_v1','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
}

// SeedAppOrigins backfills the public/self-serve split for catalogs created
// before apps carried an origin column. It runs once; later admin
// reclassifications are preserved because the marker is only written here.
func (s *Service) SeedAppOrigins() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='app_origin_v1'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	known := map[string]domain.AppOrigin{
		"mpd":            domain.OriginDistro,
		"upmpdcli":       domain.OriginDistro,
		"shairport-sync": domain.OriginDistro,
		"gmediarender":   domain.OriginDistro,
		"networkaudiod":  domain.OriginVendor,
		"roonbridge":     domain.OriginSelf,
	}
	for id, origin := range known {
		if _, err := s.db.Exec(`UPDATE apps SET origin=? WHERE app_id=?`, string(origin), id); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`INSERT INTO server_state(key,value) VALUES('app_origin_v1','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
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
