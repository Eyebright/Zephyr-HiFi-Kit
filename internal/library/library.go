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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/media"
	"hifihub/server/internal/mount"
	"hifihub/server/internal/mqa"
	"hifihub/server/internal/store"
)

// Mount status values.
const (
	StatusUnmounted = "unmounted"
	StatusMounted   = "mounted"
	StatusError     = "error"
	StatusArchived  = "archived"
)

// Mount is one registered SMB share. Password is write-only (json:"-").
type Mount struct {
	Kind         string     `json:"kind"`
	ID           string     `json:"id"`
	Host         string     `json:"host"`
	Share        string     `json:"share"`
	Target       string     `json:"target"`
	Username     string     `json:"username"`
	Password     string     `json:"-"`
	Domain       string     `json:"domain"`
	Version      string     `json:"version"`
	TextEncoding string     `json:"text_encoding"`
	Writable     bool       `json:"writable"`
	Status       string     `json:"status"`
	Detail       string     `json:"detail"`
	CreatedAt    int64      `json:"created_at"`
	MountedAt    int64      `json:"mounted_at"`
	LastScan     int64      `json:"last_scan"`
	ScanResult   ScanResult `json:"scan_result"`
	Reused       bool       `json:"reused,omitempty"`
}

type ScanResult struct {
	Count        int `json:"count"`
	Added        int `json:"added"`
	Updated      int `json:"updated"`
	Unchanged    int `json:"unchanged"`
	Removed      int `json:"removed"`
	CoversFilled int `json:"covers_filled"`
	TagsRepaired int `json:"tags_repaired"`
}

// AddMountRequest is the client-supplied share configuration.
type AddMountRequest struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	Host         string `json:"host"`
	Share        string `json:"share"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Domain       string `json:"domain"`
	Version      string `json:"version"`
	TextEncoding string `json:"text_encoding"`
	Writable     bool   `json:"writable"`
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
	MQA          bool   `json:"mqa"`
	MQAChecked   bool   `json:"-"`
	CueStart     uint64 `json:"cue_start,omitempty"`
	CueEnd       uint64 `json:"cue_end,omitempty"`
	Size         int64  `json:"size"`
	MtimeNS      int64  `json:"mtime_ns"`
	ScannedAt    int64  `json:"scanned_at"`
	CoverHash    string `json:"cover_hash"`
}

// Service manages mounts and the scanned track catalog.
type Service struct {
	sourceMu   sync.Mutex // serialize source lifecycle changes and scans
	metadataMu sync.Mutex // serialize scans with metadata organization apply
	artistMu   sync.Mutex // serialize artist-index rebuilds
	db         *sql.DB
	mounts     mount.Backend
	mountRoot  string
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
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	id := "mnt-" + uuid.NewString()
	m, err := s.configuredMount(id, req)
	if err != nil {
		return Mount{}, err
	}
	existing, err := s.ListMounts()
	if err != nil {
		return Mount{}, err
	}
	for _, old := range existing {
		if !sameSource(old, m) {
			continue
		}
		if old.Status == StatusArchived {
			_, err = s.db.Exec(`UPDATE mounts SET status=?,detail='',username=?,password=?,domain=?,version=?,text_encoding=?,writable=? WHERE id=?`,
				StatusUnmounted, m.Username, m.Password, m.Domain, m.Version, m.TextEncoding, boolInt(m.Writable), old.ID)
			if err != nil {
				return Mount{}, err
			}
			old, err = s.GetMount(old.ID)
			if err != nil {
				return Mount{}, err
			}
		}
		old.Reused = true
		return old, nil
	}
	_, err = s.db.Exec(`INSERT INTO mounts
		(id,host,share,target,username,password,domain,version,text_encoding,writable,status,detail,created_at,mounted_at,kind)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, m.Host, m.Share, m.Target, m.Username, m.Password, m.Domain, m.Version, m.TextEncoding,
		boolInt(m.Writable), StatusUnmounted, "", time.Now().Unix(), 0, m.Kind)
	if err != nil {
		return Mount{}, err
	}
	return s.GetMount(id)
}

const mountCols = `id,host,share,target,username,password,domain,version,text_encoding,writable,status,detail,created_at,mounted_at,kind,last_scan,scan_result`

func scanMount(row interface{ Scan(...any) error }) (Mount, error) {
	var m Mount
	var writable int
	var result string
	err := row.Scan(&m.ID, &m.Host, &m.Share, &m.Target, &m.Username, &m.Password, &m.Domain,
		&m.Version, &m.TextEncoding, &writable, &m.Status, &m.Detail, &m.CreatedAt, &m.MountedAt, &m.Kind, &m.LastScan, &result)
	if err != nil {
		return m, err
	}
	m.Writable = writable == 1
	err = json.Unmarshal([]byte(result), &m.ScanResult)
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

func (s *Service) ArchiveMount(ctx context.Context, id string) (Mount, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	m, err := s.GetMount(id)
	if err != nil {
		return Mount{}, err
	}
	if m.Kind != "local" && m.Status == StatusMounted {
		if err := s.mounts.Unmount(ctx, m.Target); err != nil {
			return Mount{}, err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mounts SET status=?,detail='',mounted_at=0,username='',password='',domain='' WHERE id=?`, StatusArchived, id)
	if err != nil {
		return Mount{}, err
	}
	return s.GetMount(id)
}

func (s *Service) DeleteMount(ctx context.Context, id string) error {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	m, err := s.GetMount(id)
	if err != nil {
		return err
	}
	if m.Kind != "local" && m.Status == StatusMounted {
		if err := s.mounts.Unmount(ctx, m.Target); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE mount_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mounts WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.pruneCovers()
	return nil
}

// Reconnect restores sources whose saved state is mounted, which recovers the
// Windows SMB session after a reboot. Intentionally unmounted or archived
// sources are left alone.
func (s *Service) Reconnect(ctx context.Context) (attempted, restored int, failures []string) {
	ms, err := s.ListMounts()
	if err != nil {
		return 0, 0, []string{err.Error()}
	}
	for _, m := range ms {
		if m.Status != StatusMounted {
			continue
		}
		attempted++
		mounted, err := s.Mount(ctx, m.ID)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if mounted.Status != StatusMounted {
			failures = append(failures, mounted.Detail)
			continue
		}
		restored++
	}
	return attempted, restored, failures
}

// Mount performs the kernel CIFS mount and records the outcome.
func (s *Service) Mount(ctx context.Context, id string) (Mount, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
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
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
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

func (s *Service) Scan(ctx context.Context, id string) (int, error) {
	result, err := s.ScanChanges(ctx, id, false)
	return result.Count, err
}

func (s *Service) ScanChanges(ctx context.Context, id string, full bool) (ScanResult, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	result := ScanResult{}
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	m, err := s.GetMount(id)
	if err != nil {
		return ScanResult{}, err
	}
	if m.Status != StatusMounted {
		return ScanResult{}, ErrNotMounted
	}
	if m.Kind != "local" {
		if err := s.mounts.Check(ctx, m.Target); err != nil {
			return ScanResult{}, err
		}
	}
	root, err := localDirectory(m.Target)
	if err != nil {
		return ScanResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return ScanResult{}, err
	}
	byPath, err := s.scanIdentities(ctx, id)
	if err != nil {
		return ScanResult{}, err
	}
	previous, err := s.ListTracks(id)
	if err != nil {
		return ScanResult{}, err
	}
	remaining := make(map[string]Track, len(previous))
	prevByRel := make(map[string]Track, len(previous))
	for _, t := range previous {
		remaining[t.RelPath] = t
		prevByRel[t.RelPath] = t
	}
	now := time.Now().Unix()
	changed := []Track{}
	backfill := []Track{}
	seen := map[string]fs.FileInfo{}
	cueJobs := []cueJob{}
	coverCache := map[string]string{}
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
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".cue") {
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			if job, ok := newCueJob(root, p, info, m.TextEncoding); ok {
				cueJobs = append(cueJobs, job)
			}
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		oldTrack, live := remaining[rel]
		delete(remaining, rel)
		seen[p] = info
		result.Count++
		if !full && live && oldTrack.MtimeNS != 0 && oldTrack.MtimeNS == info.ModTime().UnixNano() && oldTrack.Size == info.Size() {
			result.Unchanged++
			if oldTrack.CoverHash == "" {
				backfill = append(backfill, oldTrack)
			}
			return nil
		}
		pr, err := media.ProbeFile(p)
		if err != nil {
			return fmt.Errorf("probe %s: %w", p, err)
		}
		// Tagged MQA is cheap to spot; untagged streams need the first seconds of
		// PCM. Only new/changed files pay for the sample scan, and the result is
		// persisted so later plays never repeat it.
		mqaChecked := pr.MQA
		if !pr.MQA && pr.Codec == "flac" && pr.Channels == 2 && mqaRate(pr.Rate) {
			mqaChecked = true
			if res, derr := mqa.DetectFile(p); derr == nil && res.Detected {
				pr.MQA = true
			}
		}
		tags, err := media.ReadTagsWithEncoding(p, m.TextEncoding)
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return err
		}
		old, exists := byPath[rel]
		trackID, addedAt := old.ID, old.AddedAt
		if !exists {
			trackID, addedAt = "trk-"+uuid.NewString(), now
		}
		title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		if tags.Title != "" {
			title = tags.Title
		}
		// Lossy taggers write '?' for non-ASCII text; recover what the path still
		// carries instead of surfacing question marks in the library.
		title, artist, album, albumArtist := repairTrackText(title, tags.Artist, tags.Album, tags.AlbumArtist, rel)
		changed = append(changed, Track{
			ID: trackID, MountID: id, Path: p, RelPath: rel, AddedAt: addedAt,
			Title: title, Artist: artist, Album: album, Genre: tags.Genre,
			AlbumArtist: albumArtist, Composer: tags.Composer, Year: tags.Year,
			DiscNumber: tags.DiscNumber, TrackNumber: tags.TrackNumber,
			Codec: pr.Codec, Container: pr.Container, Rate: pr.Rate, Channels: pr.Channels,
			Bits: pr.Bits, TotalSamples: pr.TotalSamples, DurationMS: pr.DurationMS,
			Decodable: pr.Decodable, MQA: pr.MQA, MQAChecked: mqaChecked, Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), ScannedAt: now,
		})
		if live {
			result.Updated++
		} else {
			result.Added++
		}
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	changed, cueStats, err := expandCueJobs(ctx, root, id, cueJobs, changed, remaining, prevByRel, seen, now)
	if err != nil {
		return ScanResult{}, err
	}
	result.Added = max(0, result.Added+cueStats.added-cueStats.droppedAdded)
	result.Updated = max(0, result.Updated+cueStats.updated-cueStats.droppedUpdated)
	result.Unchanged += cueStats.unchanged
	result.Removed = len(remaining)
	resolver := metadataScanResolver(previous, changed, remaining)
	for i := range changed {
		resolver.apply(&changed[i])
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScanResult{}, err
	}
	defer tx.Rollback()
	if err := rememberTrackIdentities(ctx, tx, id); err != nil {
		return ScanResult{}, err
	}
	for _, t := range remaining {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE id=?`, t.ID); err != nil {
			return ScanResult{}, err
		}
	}
	for _, t := range changed {
		t.CoverHash, err = s.storeCover(ctx, tx, t.Path, t.Container, root, coverCache, now)
		if err != nil {
			return ScanResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracks
			(id,mount_id,path,rel_path,title,artist,album,codec,container,rate,channels,bits,
			 total_samples,duration_ms,decodable,mqa,mqa_checked,size,scanned_at,cover_hash,added_at,genre,
			 album_artist,composer,year,disc_number,track_number,mtime_ns,cue_start,cue_end)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET path=excluded.path,title=excluded.title,artist=excluded.artist,
			album=excluded.album,codec=excluded.codec,container=excluded.container,rate=excluded.rate,
			channels=excluded.channels,bits=excluded.bits,total_samples=excluded.total_samples,
			duration_ms=excluded.duration_ms,decodable=excluded.decodable,mqa=excluded.mqa,
			mqa_checked=excluded.mqa_checked,size=excluded.size,
			scanned_at=excluded.scanned_at,cover_hash=excluded.cover_hash,genre=excluded.genre,
			album_artist=excluded.album_artist,composer=excluded.composer,year=excluded.year,
			disc_number=excluded.disc_number,track_number=excluded.track_number,mtime_ns=excluded.mtime_ns,
			cue_start=excluded.cue_start,cue_end=excluded.cue_end`,
			t.ID, t.MountID, t.Path, t.RelPath, t.Title, t.Artist, t.Album, t.Codec, t.Container,
			t.Rate, t.Channels, t.Bits, t.TotalSamples, t.DurationMS, boolInt(t.Decodable), boolInt(t.MQA), boolInt(t.MQAChecked), t.Size,
			t.ScannedAt, t.CoverHash, t.AddedAt, t.Genre, t.AlbumArtist, t.Composer,
			t.Year, t.DiscNumber, t.TrackNumber, t.MtimeNS, t.CueStart, t.CueEnd); err != nil {
			return ScanResult{}, err
		}
	}
	// Album-level fill: a later sibling may have resolved art for tracks that
	// were walked earlier in the same directory (WAV before an embedded FLAC).
	for _, t := range changed {
		if t.CoverHash != "" {
			continue
		}
		hash := coverCacheHash(coverCache[filepath.Dir(t.Path)])
		if hash == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET cover_hash=? WHERE id=?`, hash, t.ID); err != nil {
			return ScanResult{}, err
		}
		result.CoversFilled++
	}
	// Unchanged tracks keep their tags, but art may have appeared (or become
	// readable) since the last scan; fill only empty covers, sidecar-only so a
	// routine sync never reopens every FLAC to re-check embedded pictures.
	for _, t := range backfill {
		dir := filepath.Dir(t.Path)
		state := coverCache[dir]
		hash := coverCacheHash(state)
		if hash == "" && state == "" {
			if c, cerr := sidecarCover(dir, root); cerr == nil {
				hash, err = coverBytes(ctx, tx, c, now)
				if err != nil {
					return ScanResult{}, err
				}
				coverCache[dir] = hash
			} else {
				coverCache[dir] = coverSidecarNone
			}
		}
		if hash == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET cover_hash=? WHERE id=?`, hash, t.ID); err != nil {
			return ScanResult{}, err
		}
		result.CoversFilled++
	}
	// Stock tracks written by lossy taggers keep '?'; the fallback only needs
	// stored paths, so no file is reopened here.
	repaired, err := s.repairStoredTags(ctx, tx)
	if err != nil {
		return ScanResult{}, err
	}
	result.TagsRepaired = repaired
	if m.Kind != "local" {
		if err := s.mounts.Check(ctx, m.Target); err != nil {
			return ScanResult{}, err
		}
	}
	finalRoot, err := os.Stat(root)
	if err != nil {
		return ScanResult{}, err
	}
	if !os.SameFile(rootInfo, finalRoot) {
		return ScanResult{}, fmt.Errorf("扫描期间源目录发生变化，保留已有索引")
	}
	for p, before := range seen {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, err
		}
		after, err := os.Lstat(p)
		if err != nil {
			return ScanResult{}, err
		}
		if !after.Mode().IsRegular() || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return ScanResult{}, fmt.Errorf("扫描期间文件发生变化，保留已有索引：%s", p)
		}
	}
	if err := rememberTrackIdentities(ctx, tx, id); err != nil {
		return ScanResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ScanResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mounts SET last_scan=?,scan_result=? WHERE id=?`, time.Now().Unix(), string(encoded), id); err != nil {
		return ScanResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScanResult{}, err
	}
	if len(changed) > 0 || len(remaining) > 0 || result.CoversFilled > 0 || result.TagsRepaired > 0 {
		if err := s.RebuildArtistIndex(ctx); err != nil {
			slog.Warn("rebuild artist index", "err", err)
		}
	}
	s.pruneCovers()
	return result, nil
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

// Directory cover cache sentinels: "-e" means the first embedded scan found no
// art, "-s" means the sidecar lookup found no image. A cached hash always wins.
const (
	coverEmbeddedNone = "\x00embed-none"
	coverSidecarNone  = "\x00sidecar-none"
)

func coverCacheHash(state string) string {
	if state == coverEmbeddedNone || state == coverSidecarNone {
		return ""
	}
	return state
}

func (s *Service) storeCover(ctx context.Context, tx *sql.Tx, p, container, root string, cache map[string]string, now int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := filepath.Dir(p)
	if hash := coverCacheHash(cache[dir]); hash != "" {
		return hash, nil
	}
	state := cache[dir]
	// Embedded art wins, but only the first FLAC in a directory is opened;
	// every sibling reuses the album-level result.
	if container == "flac" && state != coverEmbeddedNone {
		c, err := media.ReadFLACCover(p)
		if err == nil {
			hash, err := coverBytes(ctx, tx, c, now)
			if err != nil {
				return "", err
			}
			cache[dir] = hash
			return hash, nil
		}
		if !errors.Is(err, media.ErrNoCover) {
			return "", err
		}
		state = coverEmbeddedNone
		cache[dir] = state
	}
	if state != coverSidecarNone {
		c, err := sidecarCover(dir, root)
		if err == nil {
			hash, err := coverBytes(ctx, tx, c, now)
			if err != nil {
				return "", err
			}
			cache[dir] = hash
			return hash, nil
		}
		if state == coverEmbeddedNone {
			cache[dir] = coverEmbeddedNone
		} else {
			cache[dir] = coverSidecarNone
		}
	}
	return "", nil
}

// sidecarCover checks the track's folder and, for multi-disc layouts, its
// immediate parent inside the mount root.
func sidecarCover(dir, root string) (media.Cover, error) {
	if c, err := media.ReadSidecarCover(dir); err == nil {
		return c, nil
	}
	parent := filepath.Dir(dir)
	if parent != dir {
		if rel, err := filepath.Rel(root, parent); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return media.ReadSidecarCover(parent)
		}
	}
	return media.Cover{}, media.ErrNoCover
}

func coverBytes(ctx context.Context, tx *sql.Tx, c media.Cover, now int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(c.Data)
	hash := hex.EncodeToString(sum[:])
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO covers (hash,mime,data,created_at) VALUES(?,?,?,?)`,
		hash, c.MIME, c.Data, now); err != nil {
		return "", err
	}
	return hash, nil
}

// pruneCovers drops cached art only when neither tracks nor history reference it.
func (s *Service) pruneCovers() {
	_, _ = s.db.Exec(`DELETE FROM covers WHERE hash NOT IN (
		SELECT cover_hash FROM tracks WHERE cover_hash <> ''
		UNION SELECT cover_hash FROM play_history WHERE cover_hash <> ''
		UNION SELECT cover_hash FROM favorites WHERE cover_hash <> '')`)
	_, _ = s.db.Exec(`DELETE FROM cover_variants WHERE hash NOT IN (SELECT hash FROM covers)`)
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
	total_samples,duration_ms,decodable,mqa,mqa_checked,size,scanned_at,cover_hash,added_at,genre,
	album_artist,composer,year,disc_number,track_number,mtime_ns,cue_start,cue_end`

func scanTrack(row interface{ Scan(...any) error }) (Track, error) {
	var t Track
	var decodable, mqa, checked int
	err := row.Scan(&t.ID, &t.MountID, &t.Path, &t.RelPath, &t.Title, &t.Artist, &t.Album,
		&t.Codec, &t.Container, &t.Rate, &t.Channels, &t.Bits, &t.TotalSamples, &t.DurationMS,
		&decodable, &mqa, &checked, &t.Size, &t.ScannedAt, &t.CoverHash, &t.AddedAt, &t.Genre,
		&t.AlbumArtist, &t.Composer, &t.Year, &t.DiscNumber, &t.TrackNumber, &t.MtimeNS, &t.CueStart, &t.CueEnd)
	t.Decodable = decodable == 1
	t.MQA = mqa == 1
	t.MQAChecked = checked == 1
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

// MarkDSD refreshes a DSD track's technical metadata after a successful open,
// so tracks scanned by older builds become playable and show real durations.
func (s *Service) MarkDSD(trackID string, rate, channels, bits int, total uint64, durationMS int) error {
	_, err := s.db.Exec(`UPDATE tracks SET rate=?,channels=?,bits=?,total_samples=?,duration_ms=?,decodable=1 WHERE id=?`,
		rate, channels, bits, total, durationMS, trackID)
	return err
}

// MarkMQA records a sample-analysis result so later plays skip the scan.
func (s *Service) MarkMQA(trackID string, detected bool) error {
	_, err := s.db.Exec(`UPDATE tracks SET mqa=?, mqa_checked=1 WHERE id=?`, boolInt(detected), trackID)
	return err
}

func mqaRate(rate int) bool {
	switch rate {
	case 44100, 48000, 88200, 96000, 176400, 192000:
		return true
	}
	return false
}

// PlaybackPath derives the path from the current source and relative identity,
// then rechecks filesystem containment. Call immediately before every decoder
// open/reopen; cached Track.Path is not a playback authorization. This guards
// changed symlinks, not concurrent filesystem replacement after this check.
func (s *Service) PlaybackPath(trackID string) (string, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	return s.playbackPath(trackID)
}

func (s *Service) playbackPath(trackID string) (string, error) {
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
