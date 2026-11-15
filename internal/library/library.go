// Package library is the Roon-like music-server core: it registers SMB/CIFS
// media-library mounts, identifies every audio file by header (pure Go), and
// exposes the scanned track catalog for ZAT push playback. Mounting delegates to
// internal/mount (kernel mount.cifs); identification delegates to internal/media.
//
// Passwords are stored like node secrets (server-side only) and are never
// serialized to API clients: Mount.Password carries json:"-".
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/media"
	"hifihub/server/internal/mount"
	"hifihub/server/internal/store"
)

// Mount status values.
const (
	StatusUnmounted = "unmounted"
	StatusMounted   = "mounted"
	StatusError     = "error"
)

// Mount is one registered SMB share. Password is write-only (json:"-").
type Mount struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Share     string `json:"share"`
	Target    string `json:"target"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	Domain    string `json:"domain"`
	Version   string `json:"version"`
	Writable  bool   `json:"writable"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	CreatedAt int64  `json:"created_at"`
	MountedAt int64  `json:"mounted_at"`
}

// AddMountRequest is the client-supplied share configuration.
type AddMountRequest struct {
	Host     string `json:"host"`
	Share    string `json:"share"`
	Username string `json:"username"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
	Version  string `json:"version"`
	Writable bool   `json:"writable"`
}

// Track is one identified audio file in a mounted library.
type Track struct {
	ID           string `json:"id"`
	MountID      string `json:"mount_id"`
	Path         string `json:"path"`
	RelPath      string `json:"rel_path"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	Codec        string `json:"codec"`
	Container    string `json:"container"`
	Rate         int    `json:"rate"`
	Channels     int    `json:"channels"`
	Bits         int    `json:"bits"`
	TotalSamples uint64 `json:"total_samples"`
	DurationMS   int    `json:"duration_ms"`
	Decodable    bool   `json:"decodable"`
	Size         int64  `json:"size"`
	ScannedAt    int64  `json:"scanned_at"`
}

// Service manages mounts and the scanned track catalog.
type Service struct {
	db        *sql.DB
	mounts    *mount.Manager
	mountRoot string
}

// New builds a library service. mountRoot is the parent directory under which
// each share is mounted (one subdirectory per mount id).
func New(s *store.Store, m *mount.Manager, mountRoot string) *Service {
	return &Service{db: s.DB, mounts: m, mountRoot: mountRoot}
}

// spec builds the mount.Spec for a stored mount row.
func (m Mount) spec() mount.Spec {
	return mount.Spec{
		Host: m.Host, Share: m.Share, Target: m.Target,
		Username: m.Username, Password: m.Password, Domain: m.Domain,
		Version: m.Version, Writable: m.Writable,
	}
}

// AddMount validates and registers a share (unmounted). The local mount point is
// mountRoot/<id>; path.Join keeps Linux-style forward slashes on every host.
func (s *Service) AddMount(req AddMountRequest) (Mount, error) {
	var out Mount
	id := "mnt-" + uuid.NewString()
	target := path.Join(s.mountRoot, id)
	spec := mount.Spec{
		Host: req.Host, Share: req.Share, Target: target,
		Username: req.Username, Password: req.Password, Domain: req.Domain,
		Version: req.Version, Writable: req.Writable,
	}
	if err := spec.Validate(); err != nil {
		return out, err
	}
	ver := req.Version
	if ver == "" {
		ver = mount.DefaultVersion
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO mounts
		(id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, req.Host, req.Share, target, req.Username, req.Password, req.Domain, ver,
		boolInt(req.Writable), StatusUnmounted, "", now, 0)
	if err != nil {
		return out, err
	}
	return s.GetMount(id)
}

const mountCols = `id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at`

func scanMount(row interface{ Scan(...any) error }) (Mount, error) {
	var m Mount
	var writable int
	err := row.Scan(&m.ID, &m.Host, &m.Share, &m.Target, &m.Username, &m.Password, &m.Domain,
		&m.Version, &writable, &m.Status, &m.Detail, &m.CreatedAt, &m.MountedAt)
	m.Writable = writable == 1
	return m, err
}

// GetMount returns one mount (password included for internal use; the API layer
// re-marshals Mount which drops it).
func (s *Service) GetMount(id string) (Mount, error) {
	row := s.db.QueryRow(`SELECT `+mountCols+` FROM mounts WHERE id=?`, id)
	m, err := scanMount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m, catalog.ErrNotFound
	}
	return m, err
}

// ListMounts returns all mounts ordered by creation time.
func (s *Service) ListMounts() ([]Mount, error) {
	rows, err := s.db.Query(`SELECT ` + mountCols + ` FROM mounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mount{}
	for rows.Next() {
		m, err := scanMount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMount unmounts (best effort) and removes the share and its tracks.
func (s *Service) DeleteMount(ctx context.Context, id string) error {
	m, err := s.GetMount(id)
	if err != nil {
		return err
	}
	if m.Status == StatusMounted {
		_ = s.mounts.Unmount(ctx, m.Target)
	}
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE mount_id=?`, id); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM mounts WHERE id=?`, id)
	return err
}

// Mount performs the kernel CIFS mount and records the outcome.
func (s *Service) Mount(ctx context.Context, id string) (Mount, error) {
	m, err := s.GetMount(id)
	if err != nil {
		return m, err
	}
	// Best-effort: mount.cifs needs this dir and surfaces the real error if absent.
	_ = os.MkdirAll(m.Target, 0o755)
	if err := s.mounts.Mount(ctx, m.spec()); err != nil {
		s.setStatus(id, StatusError, err.Error(), 0)
		return s.GetMount(id)
	}
	now := time.Now().Unix()
	s.setStatus(id, StatusMounted, "", now)
	return s.GetMount(id)
}

// Unmount releases the share and records the outcome.
func (s *Service) Unmount(ctx context.Context, id string) (Mount, error) {
	m, err := s.GetMount(id)
	if err != nil {
		return m, err
	}
	if err := s.mounts.Unmount(ctx, m.Target); err != nil {
		s.setStatus(id, StatusError, err.Error(), 0)
		return s.GetMount(id)
	}
	s.setStatus(id, StatusUnmounted, "", 0)
	return s.GetMount(id)
}

func (s *Service) setStatus(id, status, detail string, mountedAt int64) {
	_, _ = s.db.Exec(`UPDATE mounts SET status=?,detail=?,mounted_at=? WHERE id=?`,
		status, detail, mountedAt, id)
}

// audioExts are the file extensions the scanner catalogues. Identification (not
// the extension) decides the codec; the extension only gates which files we probe.
var audioExts = map[string]bool{
	".flac": true, ".wav": true, ".wave": true, ".mp3": true, ".ogg": true, ".oga": true,
	".opus": true, ".m4a": true, ".mp4": true, ".aac": true, ".adts": true, ".dsf": true,
	".dff": true, ".aiff": true, ".aif": true, ".wma": true, ".alac": true,
}

// Scan walks the mounted share, identifies every audio file and replaces the
// track catalog for that mount. It returns the number of tracks catalogued.
// Folder convention (Roon-like): Artist/Album/Track — artist is the grandparent
// directory, album the parent, title the filename without extension.
func (s *Service) Scan(ctx context.Context, id string) (int, error) {
	m, err := s.GetMount(id)
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	tracks := []Track{}
	err = filepath.WalkDir(m.Target, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		pr, perr := media.ProbeFile(p)
		if perr != nil {
			pr = media.ProbeResult{Codec: "unknown"}
		}
		rel, err := filepath.Rel(m.Target, p)
		if err != nil {
			rel = d.Name()
		}
		title, artist, album := deriveMeta(m.Target, p)
		tracks = append(tracks, Track{
			ID: "trk-" + uuid.NewString(), MountID: id, Path: p, RelPath: filepath.ToSlash(rel),
			Title: title, Artist: artist, Album: album,
			Codec: pr.Codec, Container: pr.Container, Rate: pr.Rate, Channels: pr.Channels,
			Bits: pr.Bits, TotalSamples: pr.TotalSamples, DurationMS: pr.DurationMS,
			Decodable: pr.Decodable, Size: info.Size(), ScannedAt: now,
		})
		return nil
	})
	if err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE mount_id=?`, id); err != nil {
		return 0, err
	}
	for _, t := range tracks {
		if _, err := s.db.Exec(`INSERT INTO tracks
			(id,mount_id,path,rel_path,title,artist,album,codec,container,rate,channels,bits,
			 total_samples,duration_ms,decodable,size,scanned_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.MountID, t.Path, t.RelPath, t.Title, t.Artist, t.Album, t.Codec, t.Container,
			t.Rate, t.Channels, t.Bits, t.TotalSamples, t.DurationMS, boolInt(t.Decodable), t.Size, t.ScannedAt); err != nil {
			return 0, err
		}
	}
	return len(tracks), nil
}

// deriveMeta extracts title/artist/album from the path relative to the share root.
func deriveMeta(root, p string) (title, artist, album string) {
	base := filepath.Base(p)
	title = strings.TrimSuffix(base, filepath.Ext(base))
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return title, "", ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	// parts[len-1] is the filename; the two directories above it are album/artist.
	switch {
	case len(parts) >= 3:
		album = parts[len(parts)-2]
		artist = parts[len(parts)-3]
	case len(parts) == 2:
		album = parts[0]
	}
	return title, artist, album
}

const trackCols = `id,mount_id,path,rel_path,title,artist,album,codec,container,rate,channels,bits,
	total_samples,duration_ms,decodable,size,scanned_at`

func scanTrack(row interface{ Scan(...any) error }) (Track, error) {
	var t Track
	var decodable int
	err := row.Scan(&t.ID, &t.MountID, &t.Path, &t.RelPath, &t.Title, &t.Artist, &t.Album,
		&t.Codec, &t.Container, &t.Rate, &t.Channels, &t.Bits, &t.TotalSamples, &t.DurationMS,
		&decodable, &t.Size, &t.ScannedAt)
	t.Decodable = decodable == 1
	return t, err
}

// ListTracks returns the catalog for a mount (all mounts when id is empty),
// ordered by relative path.
func (s *Service) ListTracks(mountID string) ([]Track, error) {
	q := `SELECT ` + trackCols + ` FROM tracks`
	args := []any{}
	if mountID != "" {
		q += ` WHERE mount_id=?`
		args = append(args, mountID)
	}
	q += ` ORDER BY rel_path`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTrack returns one track, including its absolute file path for decoding.
func (s *Service) GetTrack(id string) (Track, error) {
	row := s.db.QueryRow(`SELECT `+trackCols+` FROM tracks WHERE id=?`, id)
	t, err := scanTrack(row)
	if errors.Is(err, sql.ErrNoRows) {
		return t, catalog.ErrNotFound
	}
	return t, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ErrNotMounted is returned when playback/scan needs a live mount.
var ErrNotMounted = fmt.Errorf("共享未挂载")
