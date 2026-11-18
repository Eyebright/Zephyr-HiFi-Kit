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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	Kind      string `json:"kind"`
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
	Kind     string `json:"kind"`
	Path     string `json:"path"`
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
	Genre        string `json:"genre"`
	AlbumArtist  string `json:"album_artist"`
	Composer     string `json:"composer"`
	Year         int    `json:"year"`
	DiscNumber   int    `json:"disc_number"`
	TrackNumber  int    `json:"track_number"`
	AddedAt      int64  `json:"added_at"`
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
	CoverHash    string `json:"cover_hash"`
}

// Service manages mounts and the scanned track catalog.
type Service struct {
	sourceMu  sync.Mutex // serialize source lifecycle changes and scans
	db        *sql.DB
	mounts    mount.Backend
	mountRoot string
}

// New builds a library service. mountRoot is the parent directory under which
// each share is mounted (one subdirectory per mount id). On Windows shares are
// reached directly over UNC and mountRoot is unused.
func New(s *store.Store, m mount.Backend, mountRoot string) *Service {
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

// AddMount validates and registers a share (unmounted). On Linux the local mount
// point is mountRoot/<id>; path.Join keeps Linux-style forward slashes on every
// host. On Windows there is no kernel mount: the target is the UNC share itself,
// read directly through the system SMB client.
func (s *Service) AddMount(req AddMountRequest) (Mount, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	id := "mnt-" + uuid.NewString()
	m, err := s.configuredMount(id, req)
	if err != nil {
		return Mount{}, err
	}
	_, err = s.db.Exec(`INSERT INTO mounts
		(id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at,kind)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, m.Host, m.Share, m.Target, m.Username, m.Password, m.Domain, m.Version,
		boolInt(m.Writable), StatusUnmounted, "", time.Now().Unix(), 0, m.Kind)
	if err != nil {
		return Mount{}, err
	}
	return s.GetMount(id)
}

const mountCols = `id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at,kind`

func scanMount(row interface{ Scan(...any) error }) (Mount, error) {
	var m Mount
	var writable int
	err := row.Scan(&m.ID, &m.Host, &m.Share, &m.Target, &m.Username, &m.Password, &m.Domain,
		&m.Version, &writable, &m.Status, &m.Detail, &m.CreatedAt, &m.MountedAt, &m.Kind)
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
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	m, err := s.GetMount(id)
	if err != nil {
		return err
	}
	if m.Kind != "local" && m.Status == StatusMounted {
		_ = s.mounts.Unmount(ctx, m.Target)
	}
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE mount_id=?`, id); err != nil {
		return err
	}
	s.pruneCovers()
	_, err = s.db.Exec(`DELETE FROM mounts WHERE id=?`, id)
	return err
}

// Mount performs the kernel CIFS mount and records the outcome.
func (s *Service) Mount(ctx context.Context, id string) (Mount, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Mount{}, err
	}
	m, err := s.GetMount(id)
	if err != nil {
		return m, err
	}
	if m.Kind == "local" {
		if _, err := localDirectory(m.Target); err != nil {
			s.setStatus(id, StatusError, err.Error(), 0)
		} else {
			s.setStatus(id, StatusMounted, "", time.Now().Unix())
		}
		return s.GetMount(id)
	}
	// Best-effort: mount.cifs needs this dir and surfaces the real error if absent.
	// UNC targets are remote shares; creating them locally would be wrong.
	if !mount.IsUNC(m.Target) {
		_ = os.MkdirAll(m.Target, 0o755)
	}
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
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Mount{}, err
	}
	m, err := s.GetMount(id)
	if err != nil {
		return m, err
	}
	if m.Kind == "local" {
		s.setStatus(id, StatusUnmounted, "", 0)
		return s.GetMount(id)
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

// Scan preserves identity and first-added time by relative path across rescans.
func (s *Service) Scan(ctx context.Context, id string) (int, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m, err := s.GetMount(id)
	if err != nil {
		return 0, err
	}
	if m.Status != StatusMounted {
		return 0, ErrNotMounted
	}
	// Resolve the root once; never follow symlinks/junctions below it.
	root, err := localDirectory(m.Target)
	if err != nil {
		return 0, err
	}
	// Merge live rows with identities retained for missing/cleaned tracks.
	// Release rows before cover writes on the store's single connection.
	byPath, err := s.scanIdentities(ctx, id)
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	tracks := []Track{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return err
		}
		if !withinRoot(root, real) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		pr, err := media.ProbeFile(p)
		if err != nil {
			return fmt.Errorf("probe %s: %w", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		old, exists := byPath[rel]
		trackID, addedAt := old.ID, old.AddedAt
		if !exists {
			trackID, addedAt = "trk-"+uuid.NewString(), now
		}
		title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		artist, album := "", ""
		tags, _ := media.ReadTags(p)
		if tags.Title != "" {
			title = tags.Title
		}
		if tags.Artist != "" {
			artist = tags.Artist
		}
		if tags.Album != "" {
			album = tags.Album
		}
		tracks = append(tracks, Track{
			ID: trackID, MountID: id, Path: p, RelPath: rel, AddedAt: addedAt,
			Title: title, Artist: artist, Album: album, Genre: tags.Genre,
			AlbumArtist: tags.AlbumArtist, Composer: tags.Composer, Year: tags.Year,
			DiscNumber: tags.DiscNumber, TrackNumber: tags.TrackNumber,
			Codec: pr.Codec, Container: pr.Container, Rate: pr.Rate, Channels: pr.Channels,
			Bits: pr.Bits, TotalSamples: pr.TotalSamples, DurationMS: pr.DurationMS,
			Decodable: pr.Decodable, Size: info.Size(), ScannedAt: now,
			CoverHash: s.storeCover(p, pr, now),
		})
		return nil
	})
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := rememberTrackIdentities(ctx, tx, id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE mount_id=?`, id); err != nil {
		return 0, err
	}
	for _, t := range tracks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracks
			(id,mount_id,path,rel_path,title,artist,album,codec,container,rate,channels,bits,
			 total_samples,duration_ms,decodable,size,scanned_at,cover_hash,added_at,genre,
			 album_artist,composer,year,disc_number,track_number)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.MountID, t.Path, t.RelPath, t.Title, t.Artist, t.Album, t.Codec, t.Container,
			t.Rate, t.Channels, t.Bits, t.TotalSamples, t.DurationMS, boolInt(t.Decodable), t.Size,
			t.ScannedAt, t.CoverHash, t.AddedAt, t.Genre, t.AlbumArtist, t.Composer,
			t.Year, t.DiscNumber, t.TrackNumber); err != nil {
			return 0, err
		}
	}
	if err := rememberTrackIdentities(ctx, tx, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.pruneCovers()
	return len(tracks), nil
}

// Live catalog rows take precedence (including legacy added_at=0). Missing rows
// remain in track_identities until their source is explicitly deleted.
const allTrackIdentities = `SELECT id,mount_id,rel_path,added_at FROM tracks
	UNION ALL SELECT id,mount_id,rel_path,added_at FROM track_identities i
	WHERE NOT EXISTS(SELECT 1 FROM tracks t WHERE t.mount_id=i.mount_id AND t.rel_path=i.rel_path)`

func (s *Service) scanIdentities(ctx context.Context, mountID string) (map[string]backupTrackIdentity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,mount_id,rel_path,added_at FROM (`+allTrackIdentities+`) WHERE mount_id=?`, mountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]backupTrackIdentity{}
	for rows.Next() {
		var v backupTrackIdentity
		if err := rows.Scan(&v.ID, &v.MountID, &v.RelPath, &v.AddedAt); err != nil {
			return nil, err
		}
		out[v.RelPath] = v
	}
	return out, rows.Err()
}

func rememberTrackIdentities(ctx context.Context, tx *sql.Tx, mountID string) error {
	where := "1=1" // disambiguate SQLite's INSERT SELECT ... ON CONFLICT syntax
	args := []any{}
	if mountID != "" {
		where = "mount_id=?"
		args = append(args, mountID)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO track_identities(id,mount_id,rel_path,added_at)
		SELECT id,mount_id,rel_path,added_at FROM tracks WHERE `+where+`
		ON CONFLICT(mount_id,rel_path) DO UPDATE SET id=excluded.id,added_at=excluded.added_at`, args...)
	return err
}

// storeCover extracts embedded FLAC art and caches it content-addressed. Cover
// failures never fail a scan: the track simply keeps its placeholder art.
func (s *Service) storeCover(p string, pr media.ProbeResult, now int64) string {
	if pr.Container != "flac" {
		return ""
	}
	c, err := media.ReadFLACCover(p)
	if err != nil || len(c.Data) == 0 {
		return ""
	}
	sum := sha256.Sum256(c.Data)
	hash := hex.EncodeToString(sum[:])
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO covers (hash,mime,data,created_at) VALUES(?,?,?,?)`,
		hash, c.MIME, c.Data, now); err != nil {
		return ""
	}
	return hash
}

// pruneCovers drops cached art only when neither tracks nor history reference it.
func (s *Service) pruneCovers() {
	_, _ = s.db.Exec(`DELETE FROM covers WHERE hash NOT IN (
		SELECT cover_hash FROM tracks WHERE cover_hash <> ''
		UNION SELECT cover_hash FROM play_history WHERE cover_hash <> ''
		UNION SELECT cover_hash FROM favorites WHERE cover_hash <> '')`)
}

// GetCover returns cached album art by content hash.
func (s *Service) GetCover(hash string) (string, []byte, error) {
	var mime string
	var data []byte
	err := s.db.QueryRow(`SELECT mime,data FROM covers WHERE hash=?`, hash).Scan(&mime, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, catalog.ErrNotFound
	}
	return mime, data, err
}

const trackCols = `id,mount_id,path,rel_path,title,artist,album,codec,container,rate,channels,bits,
	total_samples,duration_ms,decodable,size,scanned_at,cover_hash,added_at,genre,
	album_artist,composer,year,disc_number,track_number`

func scanTrack(row interface{ Scan(...any) error }) (Track, error) {
	var t Track
	var decodable int
	err := row.Scan(&t.ID, &t.MountID, &t.Path, &t.RelPath, &t.Title, &t.Artist, &t.Album,
		&t.Codec, &t.Container, &t.Rate, &t.Channels, &t.Bits, &t.TotalSamples, &t.DurationMS,
		&decodable, &t.Size, &t.ScannedAt, &t.CoverHash, &t.AddedAt, &t.Genre,
		&t.AlbumArtist, &t.Composer, &t.Year, &t.DiscNumber, &t.TrackNumber)
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

// PlaybackPath derives the path from the current source and relative identity,
// then rechecks filesystem containment. Call immediately before every decoder
// open/reopen; cached Track.Path is not a playback authorization. This guards
// changed symlinks, not concurrent filesystem replacement after this check.
func (s *Service) PlaybackPath(trackID string) (string, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	t, err := s.GetTrack(trackID)
	if err != nil {
		return "", err
	}
	m, err := s.GetMount(t.MountID)
	if err != nil {
		return "", err
	}
	if m.Status != StatusMounted {
		return "", ErrNotMounted
	}
	if !backupRelPath(t.RelPath) {
		return "", fmt.Errorf("invalid track relative path")
	}
	root, err := localDirectory(m.Target)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(t.RelPath)))
	if err != nil {
		return "", err
	}
	if !withinRoot(root, real) {
		return "", fmt.Errorf("track escapes source directory")
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("track is not a regular file")
	}
	return real, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ErrNotMounted is returned when playback/scan needs a live mount.
var ErrNotMounted = fmt.Errorf("共享未挂载")
