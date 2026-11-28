package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"hifihub/server/internal/media"
	"hifihub/server/internal/mount"
)

const MaxLibraryBackupBytes = 64 << 20
const maxBackupRecords = 200000
const maxBackupCoverBytes = 32 << 20

var ErrInvalidLibraryBackup = errors.New("invalid library backup")
var ErrLibraryMaintenance = errors.New("library maintenance failed")
var ErrLibraryBusy = errors.New("library scan or source operation in progress")
var ErrInvalidLibraryCleanup = errors.New("invalid cleanup kind or cutoff")

// These DTOs and SQL column lists are deliberately independent of Mount/Track:
// adding a credential or runtime field to those types cannot leak it in backups.
type backupSource struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Host      string `json:"host"`
	Share     string `json:"share"`
	Target    string `json:"target"`
	Version   string `json:"version"`
	Writable  bool   `json:"writable"`
	Archived  bool   `json:"archived,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type backupTrack struct {
	ID           string `json:"id"`
	MountID      string `json:"mount_id"`
	Path         string `json:"path"`
	RelPath      string `json:"rel_path"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	AlbumArtist  string `json:"album_artist"`
	Composer     string `json:"composer"`
	Year         int64  `json:"year"`
	DiscNumber   int64  `json:"disc_number"`
	TrackNumber  int64  `json:"track_number"`
	Genre        string `json:"genre"`
	AddedAt      int64  `json:"added_at"`
	Codec        string `json:"codec"`
	Container    string `json:"container"`
	Rate         int64  `json:"rate"`
	Channels     int64  `json:"channels"`
	Bits         int64  `json:"bits"`
	TotalSamples int64  `json:"total_samples"`
	CueStart     int64  `json:"cue_start,omitempty"`
	CueEnd       int64  `json:"cue_end,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	Decodable    bool   `json:"decodable"`
	Size         int64  `json:"size"`
	MtimeNS      int64  `json:"mtime_ns,omitempty"`
	ScannedAt    int64  `json:"scanned_at"`
	CoverHash    string `json:"cover_hash"`
}

type backupTrackIdentity struct {
	ID      string `json:"id"`
	MountID string `json:"mount_id"`
	RelPath string `json:"rel_path"`
	AddedAt int64  `json:"added_at"`
}

type backupCover struct {
	Hash      string `json:"hash"`
	MIME      string `json:"mime"`
	Data      []byte `json:"data"`
	CreatedAt int64  `json:"created_at"`
}

type backupPlaylist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type backupPlaylistItem struct {
	PlaylistID string `json:"playlist_id"`
	Position   int64  `json:"position"`
	TrackID    string `json:"track_id"`
}

type backupHistory struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	TrackID   string `json:"track_id"`
	StartedAt int64  `json:"started_at"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Genre     string `json:"genre"`
	CoverHash string `json:"cover_hash"`
	Measured  bool   `json:"measured"`
}

type backupDaily struct {
	HistoryID    string `json:"history_id"`
	Day          int64  `json:"day"`
	ListenMS     int64  `json:"listen_ms"`
	LastPlayedAt int64  `json:"last_played_at"`
}

type libraryBackup struct {
	SchemaVersion   int                   `json:"schema_version"`
	Sources         []backupSource        `json:"sources"`
	Tracks          []backupTrack         `json:"tracks"`
	TrackIdentities []backupTrackIdentity `json:"track_identities"`
	Covers          []backupCover         `json:"covers"`
	Playlists       []backupPlaylist      `json:"playlists"`
	PlaylistItems   []backupPlaylistItem  `json:"playlist_items"`
	History         []backupHistory       `json:"play_history"`
	Daily           []backupDaily         `json:"listen_daily"`
}

// Only optional, explicitly allowlisted columns are probed. Never SELECT * or
// enumerate arbitrary columns into an export, even on newer database schemas.
func maintenanceColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func maintenanceOptional(cols map[string]bool, name, fallback string) string {
	if cols[name] {
		return name
	}
	return fallback
}

func backupReadRows(ctx context.Context, tx *sql.Tx, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		if n > maxBackupRecords {
			return ErrInvalidLibraryBackup
		}
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Account for encoded row sizes while reading, rather than accumulating an
// unbounded catalog and only noticing the size limit at the final marshal.
func appendBackupRecord[T any](rows *[]T, value T, used *int) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ErrInvalidLibraryBackup
	}
	*used += len(encoded) + 1
	if *used > MaxLibraryBackupBytes {
		return ErrInvalidLibraryBackup
	}
	*rows = append(*rows, value)
	return nil
}

// ExportLibrary takes one consistent database snapshot; no filesystem, mount,
// credential, node, queue, server_state or audit export is performed.
func (s *Service) ExportLibrary(ctx context.Context) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrLibraryMaintenance
	}
	defer tx.Rollback()
	encodedBytes := 1024 // object keys, array delimiters and schema version
	b := libraryBackup{SchemaVersion: 1, Sources: []backupSource{}, Tracks: []backupTrack{}, TrackIdentities: []backupTrackIdentity{}, Covers: []backupCover{},
		Playlists: []backupPlaylist{}, PlaylistItems: []backupPlaylistItem{}, History: []backupHistory{}, Daily: []backupDaily{}}
	mountCols, err := maintenanceColumns(ctx, tx, "mounts")
	if err != nil {
		return nil, ErrLibraryMaintenance
	}
	trackCols, err := maintenanceColumns(ctx, tx, "tracks")
	if err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT id,`+maintenanceOptional(mountCols, "kind", "'smb'")+`,host,share,target,version,writable,created_at,status='archived' FROM mounts ORDER BY id`, func(r *sql.Rows) error {
		var v backupSource
		if err := r.Scan(&v.ID, &v.Kind, &v.Host, &v.Share, &v.Target, &v.Version, &v.Writable, &v.CreatedAt, &v.Archived); err != nil {
			return err
		}
		return appendBackupRecord(&b.Sources, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	query := `SELECT id,mount_id,path,rel_path,title,artist,album,` + maintenanceOptional(trackCols, "album_artist", "''") + `,` + maintenanceOptional(trackCols, "composer", "''") + `,` + maintenanceOptional(trackCols, "year", "0") + `,` + maintenanceOptional(trackCols, "disc_number", "0") + `,` + maintenanceOptional(trackCols, "track_number", "0") + `,genre,added_at,codec,container,rate,channels,bits,total_samples,duration_ms,decodable,size,` + maintenanceOptional(trackCols, "mtime_ns", "0") + `,` + maintenanceOptional(trackCols, "cue_start", "0") + `,` + maintenanceOptional(trackCols, "cue_end", "0") + `,scanned_at,cover_hash FROM tracks ORDER BY id`
	if err := backupReadRows(ctx, tx, query, func(r *sql.Rows) error {
		var v backupTrack
		if err := r.Scan(&v.ID, &v.MountID, &v.Path, &v.RelPath, &v.Title, &v.Artist, &v.Album, &v.AlbumArtist, &v.Composer, &v.Year, &v.DiscNumber, &v.TrackNumber, &v.Genre, &v.AddedAt, &v.Codec, &v.Container, &v.Rate, &v.Channels, &v.Bits, &v.TotalSamples, &v.DurationMS, &v.Decodable, &v.Size, &v.MtimeNS, &v.CueStart, &v.CueEnd, &v.ScannedAt, &v.CoverHash); err != nil {
			return err
		}
		return appendBackupRecord(&b.Tracks, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT id,mount_id,rel_path,added_at FROM (`+allTrackIdentities+`) ORDER BY id`, func(r *sql.Rows) error {
		var v backupTrackIdentity
		if err := r.Scan(&v.ID, &v.MountID, &v.RelPath, &v.AddedAt); err != nil {
			return err
		}
		return appendBackupRecord(&b.TrackIdentities, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	var coverBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(data)),0) FROM covers`).Scan(&coverBytes); err != nil || coverBytes > maxBackupCoverBytes {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT hash,mime,data,created_at FROM covers ORDER BY hash`, func(r *sql.Rows) error {
		var v backupCover
		if err := r.Scan(&v.Hash, &v.MIME, &v.Data, &v.CreatedAt); err != nil {
			return err
		}
		return appendBackupRecord(&b.Covers, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT id,name,created_at,updated_at FROM playlists ORDER BY id`, func(r *sql.Rows) error {
		var v backupPlaylist
		if err := r.Scan(&v.ID, &v.Name, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return err
		}
		return appendBackupRecord(&b.Playlists, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT playlist_id,position,track_id FROM playlist_items ORDER BY playlist_id,position`, func(r *sql.Rows) error {
		var v backupPlaylistItem
		if err := r.Scan(&v.PlaylistID, &v.Position, &v.TrackID); err != nil {
			return err
		}
		return appendBackupRecord(&b.PlaylistItems, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT id,node_id,track_id,started_at,title,artist,album,genre,cover_hash,measured FROM play_history ORDER BY id`, func(r *sql.Rows) error {
		var v backupHistory
		if err := r.Scan(&v.ID, &v.NodeID, &v.TrackID, &v.StartedAt, &v.Title, &v.Artist, &v.Album, &v.Genre, &v.CoverHash, &v.Measured); err != nil {
			return err
		}
		return appendBackupRecord(&b.History, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := backupReadRows(ctx, tx, `SELECT history_id,day,listen_ms,last_played_at FROM listen_daily ORDER BY history_id,day`, func(r *sql.Rows) error {
		var v backupDaily
		if err := r.Scan(&v.HistoryID, &v.Day, &v.ListenMS, &v.LastPlayedAt); err != nil {
			return err
		}
		return appendBackupRecord(&b.Daily, v, &encodedBytes)
	}); err != nil {
		return nil, ErrLibraryMaintenance
	}
	if err := validateLibraryBackup(&b); err != nil {
		return nil, err
	}
	data, err := json.Marshal(b)
	if err != nil || len(data) > MaxLibraryBackupBytes {
		return nil, ErrInvalidLibraryBackup
	}
	if err := tx.Commit(); err != nil {
		return nil, ErrLibraryMaintenance
	}
	return data, nil
}

func backupText(max int, values ...string) bool {
	for _, v := range values {
		if len(v) > max || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return false
		}
	}
	return true
}

// Backup paths must be unambiguous on both supported platforms. In particular,
// Windows drive/UNC/device/ADS paths must never become relative paths on Linux.
func backupPathText(v string) bool {
	return backupText(8192, v) && strings.IndexFunc(v, unicode.IsControl) < 0
}

func backupPathSegment(v string) bool {
	if v == "" || v == "." || v == ".." || strings.ContainsAny(v, `/\:*?"<>|`) || strings.HasSuffix(v, ".") || strings.HasSuffix(v, " ") || !backupPathText(v) {
		return false
	}
	// Windows device names alias devices even when an extension is present.
	base := strings.ToUpper(strings.SplitN(v, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		if strings.ContainsAny(strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT"), "¹²³") {
			return false
		}
	}
	return true
}

func backupSourceID(id string) bool {
	if !backupText(256, id) || !backupPathSegment(id) {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func backupRelPath(p string) bool {
	if p == "." || !fs.ValidPath(p) || !backupPathText(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if !backupPathSegment(segment) {
			return false
		}
	}
	return true
}

// No stat or symlink resolution here: portable imports can reference offline
// directories. Playback must resolve and confine the path again before opening.
func backupLocalTarget(p string) bool {
	if !backupPathText(p) || !filepath.IsAbs(p) || filepath.Clean(p) != filepath.FromSlash(p) {
		return false
	}
	if runtime.GOOS == "windows" {
		normal := strings.ReplaceAll(p, `\`, "/")
		if strings.HasPrefix(normal, "//?/") || strings.HasPrefix(normal, "//./") {
			return false
		}
		// Reject alternate data streams and reserved device names below the volume.
		rest := strings.TrimPrefix(filepath.FromSlash(p), filepath.VolumeName(p))
		for _, segment := range strings.Split(strings.TrimLeft(rest, `\`), `\`) {
			if segment != "" && !backupPathSegment(segment) {
				return false
			}
		}
	}
	return true
}

func validateBackupSource(v backupSource) bool {
	if !backupSourceID(v.ID) || !backupText(1024, v.Host, v.Share, v.Version) || !backupPathText(v.Target) || v.CreatedAt < 0 {
		return false
	}
	switch v.Kind {
	case "smb":
		// Validate options and host/share without trusting the exported mount point.
		if !backupPathSegment(v.Host) || !backupPathSegment(v.Share) {
			return false
		}
		return (mount.Spec{Host: v.Host, Share: v.Share, Target: "//" + v.Host + "/" + v.Share, Version: v.Version, Writable: v.Writable}).Validate() == nil
	case "local":
		return v.Host == "localhost" && v.Share != "" && backupPathText(v.Share) && !strings.ContainsAny(v.Share, `/\`) && v.Version == "" && backupLocalTarget(v.Target)
	default:
		return false
	}
}

func (s *Service) restoredSource(v backupSource) (backupSource, error) {
	if v.Kind == "smb" {
		v.Target = path.Join(s.mountRoot, v.ID)
		if runtime.GOOS == "windows" {
			v.Target = "//" + v.Host + "/" + v.Share
		} else if !backupLocalTarget(s.mountRoot) {
			return v, ErrInvalidLibraryBackup
		}
		if v.Version == "" {
			v.Version = mount.DefaultVersion
		}
		if err := (mount.Spec{Host: v.Host, Share: v.Share, Target: v.Target, Version: v.Version, Writable: v.Writable}).Validate(); err != nil {
			return v, ErrInvalidLibraryBackup
		}
	} else {
		v.Target = filepath.Clean(v.Target)
	}
	return v, nil
}

func sameBackupSourceIdentity(old, next backupSource) bool {
	// Normalize native separators, but never case-fold: case-sensitive Windows
	// directories and Linux shares must not inherit another directory's identity.
	return old.Kind == next.Kind && old.Host == next.Host && old.Share == next.Share && filepath.Clean(old.Target) == filepath.Clean(next.Target)
}

func validateLibraryBackup(b *libraryBackup) error {
	if b.SchemaVersion != 1 || b.Sources == nil || b.Tracks == nil || b.TrackIdentities == nil || b.Covers == nil || b.Playlists == nil || b.PlaylistItems == nil || b.History == nil || b.Daily == nil {
		return ErrInvalidLibraryBackup
	}
	if len(b.Sources)+len(b.Tracks)+len(b.TrackIdentities)+len(b.Covers)+len(b.Playlists)+len(b.PlaylistItems)+len(b.History)+len(b.Daily) > maxBackupRecords {
		return ErrInvalidLibraryBackup
	}
	sources, tracks, covers, playlists, history := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	add := func(set map[string]bool, id string) bool {
		if id == "" || !backupText(256, id) || set[id] {
			return false
		}
		set[id] = true
		return true
	}
	for _, v := range b.Sources {
		if !add(sources, v.ID) || !validateBackupSource(v) {
			return ErrInvalidLibraryBackup
		}
	}
	coverBytes := 0
	for _, v := range b.Covers {
		sum := sha256.Sum256(v.Data)
		coverBytes += len(v.Data)
		if !add(covers, v.Hash) || hex.EncodeToString(sum[:]) != v.Hash || len(v.Data) == 0 || coverBytes > maxBackupCoverBytes || !media.ValidCoverData(v.MIME, v.Data) || v.CreatedAt < 0 {
			return ErrInvalidLibraryBackup
		}
	}
	identities := map[[2]string]backupTrackIdentity{}
	identityIDs := map[string]bool{}
	for _, v := range b.TrackIdentities {
		key := [2]string{v.MountID, v.RelPath}
		if !add(identityIDs, v.ID) || !sources[v.MountID] || identities[key].ID != "" || !backupRelPath(v.RelPath) || v.AddedAt < 0 {
			return ErrInvalidLibraryBackup
		}
		identities[key] = v
	}
	paths := map[[2]string]bool{}
	for _, v := range b.Tracks {
		key := [2]string{v.MountID, v.RelPath}
		if !add(tracks, v.ID) || !sources[v.MountID] || paths[key] || identities[key].ID != v.ID || identities[key].AddedAt != v.AddedAt || !backupPathText(v.Path) || !backupRelPath(v.RelPath) || !backupText(4096, v.Title, v.Artist, v.Album, v.AlbumArtist, v.Composer, v.Genre) || !backupText(128, v.Codec, v.Container) || (v.CoverHash != "" && !covers[v.CoverHash]) || v.Year < 0 || v.Year > 9999 || v.DiscNumber < 0 || v.TrackNumber < 0 || v.Rate < 0 || v.Channels < 0 || v.Bits < 0 || v.TotalSamples < 0 || v.DurationMS < 0 || v.Size < 0 || v.AddedAt < 0 || v.ScannedAt < 0 {
			return ErrInvalidLibraryBackup
		}
		paths[key] = true
	}
	for _, v := range b.Playlists {
		if !add(playlists, v.ID) || !backupText(4096, v.Name) || v.Name == "" || v.CreatedAt < 0 || v.UpdatedAt < 0 {
			return ErrInvalidLibraryBackup
		}
	}
	type positionKey struct {
		id  string
		pos int64
	}
	positions := map[positionKey]bool{}
	for _, v := range b.PlaylistItems {
		key := positionKey{v.PlaylistID, v.Position}
		// Track IDs are intentionally soft references, just like history: catalog
		// cleanup preserves playlists and a later scan can recover their tracks.
		if !playlists[v.PlaylistID] || v.TrackID == "" || !backupText(256, v.TrackID) || v.Position < 0 || positions[key] {
			return ErrInvalidLibraryBackup
		}
		positions[key] = true
	}
	for _, v := range b.History {
		if !add(history, v.ID) || v.TrackID == "" || !backupText(256, v.NodeID, v.TrackID) || !backupText(4096, v.Title, v.Artist, v.Album, v.Genre) || (v.CoverHash != "" && !covers[v.CoverHash]) || v.StartedAt < 0 {
			return ErrInvalidLibraryBackup
		}
	}
	days := map[positionKey]bool{}
	for _, v := range b.Daily {
		key := positionKey{v.HistoryID, v.Day}
		if !history[v.HistoryID] || days[key] || v.Day < 0 || v.ListenMS < 0 || v.LastPlayedAt < 0 {
			return ErrInvalidLibraryBackup
		}
		days[key] = true
	}
	return nil
}

// RestoreLibrary only replaces the music-data allowlist, in a single transaction.
// The source mutex excludes scans and source lifecycle operations. Callers must
// quiesce playback for the entire call. It never mounts sources, touches audio
// files, or regenerates historical metadata snapshots.
func (s *Service) RestoreLibrary(ctx context.Context, data []byte) error {
	if len(data) == 0 || len(data) > MaxLibraryBackupBytes {
		return ErrInvalidLibraryBackup
	}
	var b libraryBackup
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return ErrInvalidLibraryBackup
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return ErrInvalidLibraryBackup
	}
	if err := validateLibraryBackup(&b); err != nil {
		return err
	}
	sourceTargets := map[string]string{}
	for i, source := range b.Sources {
		validated, err := s.restoredSource(source)
		if err != nil {
			return err
		}
		b.Sources[i] = validated
		sourceTargets[source.ID] = validated.Target
	}
	for i := range b.Tracks {
		v := &b.Tracks[i]
		v.Path = filepath.Join(sourceTargets[v.MountID], filepath.FromSlash(v.RelPath))
	}
	if !s.sourceMu.TryLock() {
		return ErrLibraryBusy
	}
	defer s.sourceMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrLibraryMaintenance
	}
	defer tx.Rollback()
	mountCols, err := maintenanceColumns(ctx, tx, "mounts")
	if err != nil {
		return ErrLibraryMaintenance
	}
	trackCols, err := maintenanceColumns(ctx, tx, "tracks")
	if err != nil {
		return ErrLibraryMaintenance
	}
	// Refuse even omitted live SMB sources: deleting their rows would orphan a
	// mounted filesystem. The caller must explicitly unmount before importing.
	var mounted bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mounts WHERE `+maintenanceOptional(mountCols, "kind", "'smb'")+`='smb' AND status='mounted')`).Scan(&mounted); err != nil {
		return ErrLibraryMaintenance
	}
	if mounted {
		return ErrLibraryBusy
	}
	oldSources := map[string]backupSource{}
	if err := backupReadRows(ctx, tx, `SELECT id,`+maintenanceOptional(mountCols, "kind", "'smb'")+`,host,share,target,version,writable,created_at FROM mounts`, func(r *sql.Rows) error {
		var v backupSource
		if err := r.Scan(&v.ID, &v.Kind, &v.Host, &v.Share, &v.Target, &v.Version, &v.Writable, &v.CreatedAt); err != nil {
			return err
		}
		oldSources[v.ID] = v
		return nil
	}); err != nil {
		return ErrLibraryMaintenance
	}
	for _, v := range b.Sources {
		if old, exists := oldSources[v.ID]; exists && (!validateBackupSource(old) || !sameBackupSourceIdentity(old, v)) {
			return ErrInvalidLibraryBackup
		}
	}
	// All source identity checks happen before the first write, not merely before
	// commit: same-ID credentials may only survive for unchanged physical sources.
	for _, table := range []string{"listen_daily", "play_history", "playlist_items", "playlists", "tracks", "track_identities", "covers"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return ErrLibraryMaintenance
		}
	}
	sourceIDs := map[string]bool{}
	for _, v := range b.Sources {
		sourceIDs[v.ID] = true
		// UPSERT, never REPLACE: preserve same-ID credentials and parent identity.
		status := "unmounted"
		if v.Archived {
			status = "archived"
		}
		cols := `id,host,share,target,version,writable,created_at,status`
		args := []any{v.ID, v.Host, v.Share, v.Target, v.Version, v.Writable, v.CreatedAt, status}
		update := `host=excluded.host,share=excluded.share,target=excluded.target,version=excluded.version,writable=excluded.writable,created_at=excluded.created_at,status=excluded.status,detail='',mounted_at=0`
		// Scan statistics are runtime state, not part of the restored snapshot.
		if mountCols["last_scan"] {
			update += `,last_scan=0`
		}
		if mountCols["scan_result"] {
			update += `,scan_result='{}'`
		}
		if mountCols["kind"] {
			cols += `,kind`
			args = append(args, v.Kind)
			update += `,kind=excluded.kind`
		} else if v.Kind != "smb" {
			return ErrInvalidLibraryBackup
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mounts (`+cols+`) VALUES (`+backupPlaceholders(len(args))+`) ON CONFLICT(id) DO UPDATE SET `+update, args...); err != nil {
			return ErrLibraryMaintenance
		}
	}
	oldIDs := []string{}
	if err := backupReadRows(ctx, tx, `SELECT id FROM mounts`, func(r *sql.Rows) error {
		var id string
		if err := r.Scan(&id); err != nil {
			return err
		}
		if !sourceIDs[id] {
			oldIDs = append(oldIDs, id)
		}
		return nil
	}); err != nil {
		return ErrLibraryMaintenance
	}
	for _, id := range oldIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM mounts WHERE id=?`, id); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.Covers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO covers(hash,mime,data,created_at) VALUES(?,?,?,?)`, v.Hash, v.MIME, v.Data, v.CreatedAt); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.TrackIdentities {
		if _, err := tx.ExecContext(ctx, `INSERT INTO track_identities(id,mount_id,rel_path,added_at) VALUES(?,?,?,?)`, v.ID, v.MountID, v.RelPath, v.AddedAt); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.Tracks {
		cols := `id,mount_id,path,rel_path,title,artist,album,genre,added_at,codec,container,rate,channels,bits,total_samples,duration_ms,decodable,size,scanned_at,cover_hash`
		args := []any{v.ID, v.MountID, v.Path, v.RelPath, v.Title, v.Artist, v.Album, v.Genre, v.AddedAt, v.Codec, v.Container, v.Rate, v.Channels, v.Bits, v.TotalSamples, v.DurationMS, v.Decodable, v.Size, v.ScannedAt, v.CoverHash}
		for _, field := range []struct {
			name    string
			value   any
			nonzero bool
		}{
			{"album_artist", v.AlbumArtist, v.AlbumArtist != ""}, {"composer", v.Composer, v.Composer != ""}, {"year", v.Year, v.Year != 0}, {"disc_number", v.DiscNumber, v.DiscNumber != 0}, {"track_number", v.TrackNumber, v.TrackNumber != 0},
			{"mtime_ns", v.MtimeNS, v.MtimeNS != 0}, {"cue_start", v.CueStart, v.CueStart != 0}, {"cue_end", v.CueEnd, v.CueEnd != 0},
		} {
			if trackCols[field.name] {
				cols += "," + field.name
				args = append(args, field.value)
			} else if field.nonzero {
				return ErrInvalidLibraryBackup
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracks (`+cols+`) VALUES (`+backupPlaceholders(len(args))+`)`, args...); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.Playlists {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlists(id,name,created_at,updated_at) VALUES(?,?,?,?)`, v.ID, v.Name, v.CreatedAt, v.UpdatedAt); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.PlaylistItems {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items(playlist_id,position,track_id) VALUES(?,?,?)`, v.PlaylistID, v.Position, v.TrackID); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.History {
		if _, err := tx.ExecContext(ctx, `INSERT INTO play_history(id,node_id,track_id,started_at,title,artist,album,genre,cover_hash,measured) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.ID, v.NodeID, v.TrackID, v.StartedAt, v.Title, v.Artist, v.Album, v.Genre, v.CoverHash, v.Measured); err != nil {
			return ErrLibraryMaintenance
		}
	}
	for _, v := range b.Daily {
		if _, err := tx.ExecContext(ctx, `INSERT INTO listen_daily(history_id,day,listen_ms,last_played_at) VALUES(?,?,?,?)`, v.HistoryID, v.Day, v.ListenMS, v.LastPlayedAt); err != nil {
			return ErrLibraryMaintenance
		}
	}
	if err := tx.Commit(); err != nil {
		return ErrLibraryMaintenance
	}
	return nil
}

func backupPlaceholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

type LibraryMaintenanceStats struct {
	Tracks       int64 `json:"tracks"`
	Mounts       int64 `json:"mounts"`
	Playlists    int64 `json:"playlists"`
	History      int64 `json:"history"`
	Covers       int64 `json:"covers"`
	Audit        int64 `json:"audit"`
	CoversBytes  int64 `json:"covers_bytes"`
	CoversOrphan int64 `json:"covers_orphan"`
	Lyrics       int64 `json:"lyrics"`
	LyricsBytes  int64 `json:"lyrics_bytes"`
	LyricsSynced int64 `json:"lyrics_synced"`
	Albums       int64 `json:"albums"`
}

func (s *Service) LibraryMaintenanceStatus() (LibraryMaintenanceStats, error) {
	var out LibraryMaintenanceStats
	// One statement obtains counts from a consistent SQLite read snapshot.
	err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM tracks),(SELECT COUNT(*) FROM mounts),(SELECT COUNT(*) FROM playlists),(SELECT COUNT(*) FROM play_history),(SELECT COUNT(*) FROM covers),(SELECT COUNT(*) FROM audit),
		(SELECT COALESCE(SUM(LENGTH(data)),0) FROM covers),
		(SELECT COUNT(*) FROM covers c WHERE NOT EXISTS(SELECT 1 FROM tracks WHERE tracks.cover_hash=c.hash) AND NOT EXISTS(SELECT 1 FROM play_history WHERE play_history.cover_hash=c.hash) AND NOT EXISTS(SELECT 1 FROM favorites WHERE favorites.cover_hash=c.hash)),
		(SELECT COUNT(*) FROM track_lyrics),
		(SELECT COALESCE(SUM(LENGTH(text)),0) FROM track_lyrics),
		(SELECT COALESCE(SUM(synced),0) FROM track_lyrics),
		(SELECT COUNT(*) FROM (SELECT DISTINCT album, album_artist FROM tracks WHERE album<>''))`).
		Scan(&out.Tracks, &out.Mounts, &out.Playlists, &out.History, &out.Covers, &out.Audit,
			&out.CoversBytes, &out.CoversOrphan, &out.Lyrics, &out.LyricsBytes, &out.LyricsSynced, &out.Albums)
	if err != nil {
		return out, ErrLibraryMaintenance
	}
	return out, nil
}

func (s *Service) CleanLibrary(ctx context.Context, kind string, before int64) (int64, error) {
	var query string
	var args []any
	switch kind {
	case "orphan-covers":
		query = `DELETE FROM covers WHERE NOT EXISTS(SELECT 1 FROM tracks WHERE tracks.cover_hash=covers.hash) AND NOT EXISTS(SELECT 1 FROM play_history WHERE play_history.cover_hash=covers.hash) AND NOT EXISTS(SELECT 1 FROM favorites WHERE favorites.cover_hash=covers.hash)`
	case "catalog":
		query = `DELETE FROM tracks`
	case "audit":
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		if before <= 0 || before >= today {
			return 0, ErrInvalidLibraryCleanup
		}
		query = `DELETE FROM audit WHERE at<?`
		args = []any{before}
	default:
		return 0, ErrInvalidLibraryCleanup
	}
	if kind != "audit" {
		if !s.sourceMu.TryLock() {
			return 0, ErrLibraryBusy
		}
		defer s.sourceMu.Unlock()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, ErrLibraryMaintenance
	}
	defer tx.Rollback()
	if kind == "catalog" {
		if err := rememberTrackIdentities(ctx, tx, ""); err != nil {
			return 0, ErrLibraryMaintenance
		}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, ErrLibraryMaintenance
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, ErrLibraryMaintenance
	}
	if err := tx.Commit(); err != nil {
		return 0, ErrLibraryMaintenance
	}
	return n, nil
}
