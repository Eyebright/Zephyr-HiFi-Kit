package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/mount"
	"hifihub/server/internal/store"
)

func maintenanceTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "maintenance-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, mount.NewManager(&fakeRunner{}, t.TempDir()), t.TempDir()), st
}

func maintenanceExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func maintenancePNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	im.SetRGBA(0, 0, color.RGBA{R: shade, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func maintenanceFixture(t *testing.T, s *Service) {
	t.Helper()
	source, err := s.restoredSource(backupSource{ID: "m1", Kind: "smb", Host: "nas", Share: "music"})
	if err != nil {
		t.Fatal(err)
	}
	maintenanceExec(t, s.db, `INSERT INTO mounts(id,host,share,target,username,password,domain,created_at,status) VALUES('m1','nas','music',?,'test-nas-username','test-nas-password','test-nas-domain',10,'unmounted')`, source.Target)
	coverHashes := []string{}
	for _, shade := range []uint8{50, 100, 150} {
		data := maintenancePNG(t, shade)
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		coverHashes = append(coverHashes, hash)
		maintenanceExec(t, s.db, `INSERT INTO covers(hash,mime,data,created_at) VALUES(?,'image/png',?,10)`, hash, data)
	}
	maintenanceExec(t, s.db, `INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album,scanned_at,cover_hash) VALUES('t1','m1',?,'one.flac','current title','current artist','current album',10,?)`, filepath.Join(source.Target, "one.flac"), coverHashes[0])
	maintenanceExec(t, s.db, `INSERT INTO playlists(id,name,created_at,updated_at) VALUES('p1','Favorites',10,10)`)
	maintenanceExec(t, s.db, `INSERT INTO playlist_items(playlist_id,position,track_id) VALUES('p1',0,'t1')`)
	maintenanceExec(t, s.db, `INSERT INTO play_history(id,node_id,track_id,started_at,title,artist,album,genre,cover_hash,measured) VALUES('h1','old-node','missing-track',10,'historic title','historic artist','historic album','historic genre',?,1)`, coverHashes[1])
	maintenanceExec(t, s.db, `INSERT INTO listen_daily(history_id,day,listen_ms,last_played_at) VALUES('h1',0,5432,11)`)
	maintenanceExec(t, s.db, `INSERT INTO nodes(node_id,registered_at,secret) VALUES('node-preserve',10,'test-identity-token')`)
	maintenanceExec(t, s.db, `INSERT INTO server_state(key,value) VALUES('test-key','test-server-private-value')`)
	maintenanceExec(t, s.db, `INSERT INTO audit(id,action,at,params) VALUES('a1','test',1,'test-audit-secret-params')`)
	maintenanceExec(t, s.db, `INSERT INTO license(id,key) VALUES(1,'test-license-private-key')`)
}

func maintenanceExport(t *testing.T, s *Service) ([]byte, libraryBackup) {
	t.Helper()
	data, err := s.ExportLibrary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var b libraryBackup
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return data, b
}

func TestFavoriteCoverSurvivesCatalogAndCoverCleanup(t *testing.T) {
	s, st := maintenanceTestService(t)
	maintenanceFixture(t, s)
	if err := s.SaveFavorite(FavoriteRequest{Kind: "track", TrackID: "t1"}); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := st.DB.QueryRow(`SELECT cover_hash FROM tracks WHERE id='t1'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	maintenanceExec(t, st.DB, `DELETE FROM tracks WHERE id='t1'`)
	s.pruneCovers()
	if _, _, err := s.GetCover(hash); err != nil {
		t.Fatalf("favorite cover pruned: %v", err)
	}
	if _, err := s.CleanLibrary(context.Background(), "orphan-covers", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetCover(hash); err != nil {
		t.Fatalf("favorite cover cleaned: %v", err)
	}
}

func TestLibraryBackupRoundTripAndSecrets(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	data, b := maintenanceExport(t, s)
	for _, forbidden := range []string{"test-nas-username", "test-nas-password", "test-nas-domain", "test-identity-token", "test-server-private-value", "test-audit-secret-params", "test-license-private-key", `"username"`, `"password"`, `"server_state"`, `"nodes"`, `"audit"`} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("backup leaked %q", forbidden)
		}
	}
	if b.SchemaVersion != 1 || len(b.Tracks) != 1 || len(b.Sources) != 1 || b.History[0].Title != "historic title" {
		t.Fatal("incomplete backup")
	}
	b.Sources = append(b.Sources, backupSource{ID: "m2", Kind: "smb", Host: "new-nas", Share: "new-music", Target: "/test/new", Version: "3.0", CreatedAt: 11})
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	maintenanceExec(t, s.db, `INSERT INTO mounts(id,host,share,target,created_at) VALUES('obsolete','old','old','/test/old',1)`)
	if err := s.RestoreLibrary(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	var username, password, domain, status string
	if err := s.db.QueryRow(`SELECT username,password,domain,status FROM mounts WHERE id='m1'`).Scan(&username, &password, &domain, &status); err != nil {
		t.Fatal(err)
	}
	if username != "test-nas-username" || password != "test-nas-password" || domain != "test-nas-domain" || status != "unmounted" {
		t.Fatal("existing credentials not preserved or source mounted")
	}
	if err := s.db.QueryRow(`SELECT username,password,domain,status FROM mounts WHERE id='m2'`).Scan(&username, &password, &domain, &status); err != nil {
		t.Fatal(err)
	}
	if username != "" || password != "" || domain != "" || status != "unmounted" {
		t.Fatal("new source received credentials")
	}
	for _, query := range []string{`SELECT COUNT(*) FROM nodes WHERE secret='test-identity-token'`, `SELECT COUNT(*) FROM server_state WHERE value='test-server-private-value'`, `SELECT COUNT(*) FROM audit WHERE params='test-audit-secret-params'`, `SELECT COUNT(*) FROM license WHERE key='test-license-private-key'`} {
		var n int
		if err := s.db.QueryRow(query).Scan(&n); err != nil || n != 1 {
			t.Fatalf("non-music data changed: %d %v", n, err)
		}
	}
	var obsolete int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM mounts WHERE id='obsolete'`).Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatal("obsolete source retained")
	}
	_, restored := maintenanceExport(t, s)
	if restored.History[0] != b.History[0] || restored.Daily[0] != b.Daily[0] {
		t.Fatal("historical snapshot recomputed")
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after restore")
	}
}

func maintenanceRequireScanSchema(t *testing.T, s *Service) {
	t.Helper()
	var count int
	if err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM pragma_table_info('tracks') WHERE name='mtime_ns') + (SELECT COUNT(*) FROM pragma_table_info('mounts') WHERE name IN ('last_scan','scan_result'))`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatal("incremental scan migration missing")
	}
}

func TestLibraryBackupScanFieldsRoundTrip(t *testing.T) {
	const mtimeNS int64 = 1700000000123456789
	for _, status := range []string{"archived", "unmounted", "mounted", "error"} {
		for _, existing := range []bool{false, true} {
			name := status + "/" + map[bool]string{false: "new-source", true: "existing-source"}[existing]
			t.Run(name, func(t *testing.T) {
				s, _ := maintenanceTestService(t)
				maintenanceRequireScanSchema(t, s)
				maintenanceFixture(t, s)
				maintenanceExec(t, s.db, `UPDATE tracks SET mtime_ns=?,added_at=17 WHERE id='t1'`, mtimeNS)
				maintenanceExec(t, s.db, `UPDATE play_history SET track_id='t1' WHERE id='h1'`)
				maintenanceExec(t, s.db, `UPDATE mounts SET status=?,detail='old detail',mounted_at=12,last_scan=42,scan_result='{"scanned":7}' WHERE id='m1'`, status)
				data, want := maintenanceExport(t, s)
				if want.SchemaVersion != 1 || want.Tracks[0].MtimeNS != mtimeNS || want.Sources[0].Archived != (status == "archived") {
					t.Fatal("scan fields absent or incorrect")
				}
				for _, forbidden := range []string{`"last_scan"`, `"scan_result"`, `"status"`, `"detail"`, `"mounted_at"`, "test-nas-username", "test-nas-password", "test-nas-domain"} {
					if bytes.Contains(data, []byte(forbidden)) {
						t.Fatal("backup included runtime state or credentials")
					}
				}
				dest := s
				if existing {
					// An existing source's state must be replaced by the backup's flag.
					// Live SMB sources are explicitly taken offline before restoring.
					oldStatus := "archived"
					if status == "archived" {
						oldStatus = "unmounted"
					}
					maintenanceExec(t, dest.db, `UPDATE mounts SET status=? WHERE id='m1'`, oldStatus)
				} else {
					dest, _ = maintenanceTestService(t)
				}
				if err := dest.RestoreLibrary(t.Context(), data); err != nil {
					t.Fatal(err)
				}
				var gotStatus, detail, scanResult, password string
				var mountedAt, lastScan int64
				if err := dest.db.QueryRow(`SELECT status,detail,mounted_at,last_scan,scan_result,password FROM mounts WHERE id='m1'`).Scan(&gotStatus, &detail, &mountedAt, &lastScan, &scanResult, &password); err != nil {
					t.Fatal(err)
				}
				wantStatus := "unmounted"
				if status == "archived" {
					wantStatus = "archived"
				}
				if gotStatus != wantStatus || detail != "" || mountedAt != 0 || lastScan != 0 || scanResult != "{}" {
					t.Fatal("restored source has incorrect state or stale scan statistics")
				}
				if (existing && password != "test-nas-password") || (!existing && password != "") {
					t.Fatal("restore changed credential preservation semantics")
				}
				// Portable SMB paths are derived from the destination mount root.
				source, err := dest.restoredSource(want.Sources[0])
				if err != nil {
					t.Fatal(err)
				}
				want.Sources[0] = source
				want.Tracks[0].Path = filepath.Join(source.Target, filepath.FromSlash(want.Tracks[0].RelPath))
				_, got := maintenanceExport(t, dest)
				if !reflect.DeepEqual(got, want) {
					t.Fatal("round trip changed scan fields, metadata, identities or history")
				}
			})
		}
	}
}

func TestLibraryRestoreLegacyBackupScanDefaults(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceRequireScanSchema(t, s)
	maintenanceFixture(t, s)
	original, want := maintenanceExport(t, s)
	// This is the original schema-version-1 shape, with neither new field.
	if bytes.Contains(original, []byte(`"mtime_ns"`)) || bytes.Contains(original, []byte(`"archived"`)) {
		t.Fatal("zero-valued optional fields must be omitted")
	}
	maintenanceExec(t, s.db, `UPDATE tracks SET mtime_ns=1700000000123456789 WHERE id='t1'`)
	maintenanceExec(t, s.db, `UPDATE mounts SET status='archived',last_scan=42,scan_result='{"scanned":7}' WHERE id='m1'`)
	if err := s.RestoreLibrary(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	var mtimeNS, lastScan int64
	var status, scanResult string
	if err := s.db.QueryRow(`SELECT t.mtime_ns,m.status,m.last_scan,m.scan_result FROM tracks t JOIN mounts m ON m.id=t.mount_id WHERE t.id='t1'`).Scan(&mtimeNS, &status, &lastScan, &scanResult); err != nil {
		t.Fatal(err)
	}
	if mtimeNS != 0 || status != "unmounted" || lastScan != 0 || scanResult != "{}" {
		t.Fatal("legacy restore retained scan state instead of zero defaults")
	}
	after, got := maintenanceExport(t, s)
	if !bytes.Equal(original, after) || !reflect.DeepEqual(got, want) {
		t.Fatal("legacy round trip changed metadata, identities or history")
	}
}

func TestLibraryBackupWithoutScanColumns(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	// Simulate the old schema only in this test's disposable on-disk database.
	for _, field := range []struct{ table, column string }{{"tracks", "mtime_ns"}, {"mounts", "last_scan"}, {"mounts", "scan_result"}} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, field.table, field.column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			maintenanceExec(t, s.db, `ALTER TABLE `+field.table+` DROP COLUMN `+field.column)
		}
	}
	original, b := maintenanceExport(t, s)
	if b.SchemaVersion != 1 || b.Tracks[0].MtimeNS != 0 || b.Sources[0].Archived || bytes.Contains(original, []byte(`"mtime_ns"`)) || bytes.Contains(original, []byte(`"archived"`)) {
		t.Fatal("old-schema export did not use optional defaults")
	}
	if err := s.RestoreLibrary(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	after, _ := maintenanceExport(t, s)
	if !bytes.Equal(original, after) {
		t.Fatal("old-schema round trip changed music data")
	}
	b.Tracks[0].MtimeNS = 1700000000123456789
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreLibrary(t.Context(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
		t.Fatalf("new timestamp silently lost on unsupported schema: %v", err)
	}
	after, _ = maintenanceExport(t, s)
	if !bytes.Equal(original, after) {
		t.Fatal("unsupported-schema restore did not roll back")
	}
}

func TestLibraryRestoreStrictValidation(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	original, _ := maintenanceExport(t, s)
	for _, test := range []struct {
		name   string
		change func(*libraryBackup)
	}{
		{"version", func(b *libraryBackup) { b.SchemaVersion = 2 }},
		{"missing-array", func(b *libraryBackup) { b.History = nil }},
		{"missing-source", func(b *libraryBackup) { b.Tracks[0].MountID = "absent" }},
		{"missing-playlist", func(b *libraryBackup) { b.PlaylistItems[0].PlaylistID = "absent" }},
		{"missing-history", func(b *libraryBackup) { b.Daily[0].HistoryID = "absent" }},
		{"missing-cover", func(b *libraryBackup) { b.History[0].CoverHash = strings.Repeat("0", 64) }},
		{"duplicate-id", func(b *libraryBackup) { b.Tracks = append(b.Tracks, b.Tracks[0]) }},
		{"long-field", func(b *libraryBackup) { b.Tracks[0].Title = strings.Repeat("a", 4097) }},
		{"negative-listening", func(b *libraryBackup) { b.Daily[0].ListenMS = -1 }},
		{"cover-hash", func(b *libraryBackup) { b.Covers[0].Data = []byte("wrong") }},
		{"source-kind", func(b *libraryBackup) { b.Sources[0].Kind = "exec" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var b libraryBackup
			if err := json.Unmarshal(original, &b); err != nil {
				t.Fatal(err)
			}
			test.change(&b)
			data, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreLibrary(context.Background(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
				t.Fatalf("accepted bad backup: %v", err)
			}
			after, _ := maintenanceExport(t, s)
			if !bytes.Equal(original, after) {
				t.Fatal("invalid restore modified data")
			}
		})
	}
	for _, data := range [][]byte{append(append([]byte{}, original...), []byte(` {}`)...), bytes.Replace(original, []byte(`"schema_version":1`), []byte(`"schema_version":1,"password":"secret"`), 1), []byte(`null`), bytes.Repeat([]byte(" "), MaxLibraryBackupBytes+1)} {
		if err := s.RestoreLibrary(context.Background(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
			t.Fatalf("invalid JSON accepted: %v", err)
		}
	}
	for _, test := range []struct{ name, id, field string }{
		{"mtime-string", "t1", `"mtime_ns":"123"`},
		{"mtime-fraction", "t1", `"mtime_ns":1.5`},
		{"mtime-overflow", "t1", `"mtime_ns":9223372036854775808`},
		{"archived-string", "m1", `"archived":"true"`},
		{"archived-number", "m1", `"archived":1`},
		{"source-credentials", "m1", `"password":"test-secret"`},
		{"scan-time", "m1", `"last_scan":42`},
		{"scan-result", "m1", `"scan_result":{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := []byte(`"id":"` + test.id + `"`)
			data := bytes.Replace(original, id, []byte(string(id)+","+test.field), 1)
			if err := s.RestoreLibrary(t.Context(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
				t.Fatalf("invalid field accepted: %v", err)
			}
			after, _ := maintenanceExport(t, s)
			if !bytes.Equal(original, after) {
				t.Fatal("invalid field modified music data")
			}
		})
	}
}

func TestLibraryRestoreTransactionRollback(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	original, b := maintenanceExport(t, s)
	b.Sources[0].Writable = true
	b.Tracks[0].Title = "replacement"
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	// Fail the last table insertion, after the deletes and source upsert.
	maintenanceExec(t, s.db, `CREATE TRIGGER fail_restore BEFORE INSERT ON listen_daily BEGIN SELECT RAISE(ABORT,'synthetic restore failure'); END`)
	if err := s.RestoreLibrary(context.Background(), data); !errors.Is(err, ErrLibraryMaintenance) {
		t.Fatalf("expected transaction failure: %v", err)
	}
	after, _ := maintenanceExport(t, s)
	if !bytes.Equal(original, after) {
		t.Fatal("partial restore survived rollback")
	}
	var password, status string
	if err := s.db.QueryRow(`SELECT password,status FROM mounts WHERE id='m1'`).Scan(&password, &status); err != nil {
		t.Fatal(err)
	}
	if password != "test-nas-password" || status != "unmounted" {
		t.Fatal("source transaction did not roll back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RestoreLibrary(ctx, original); err == nil {
		t.Fatal("cancelled restore succeeded")
	}
}

func TestLibraryCleanupPreservesHistoryAndPlaylists(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	_, before := maintenanceExport(t, s)
	stats, err := s.LibraryMaintenanceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if stats != (LibraryMaintenanceStats{Tracks: 1, Mounts: 1, Playlists: 1, History: 1, Covers: 3, Audit: 1}) {
		t.Fatalf("stats: %+v", stats)
	}
	n, err := s.CleanLibrary(context.Background(), "catalog", 0)
	if err != nil || n != 1 {
		t.Fatalf("catalog: %d %v", n, err)
	}
	n, err = s.CleanLibrary(context.Background(), "orphan-covers", 0)
	if err != nil || n != 2 {
		t.Fatalf("orphan covers: %d %v", n, err)
	}
	_, after := maintenanceExport(t, s)
	if len(after.Tracks) != 0 || len(after.Sources) != 1 || len(after.Covers) != 1 || len(after.Playlists) != 1 || len(after.PlaylistItems) != 1 || after.History[0] != before.History[0] || after.Daily[0] != before.Daily[0] {
		t.Fatal("cleanup changed unrelated music data")
	}
	// A backup after catalog cleanup still round-trips dangling playlist IDs.
	data, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreLibrary(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	maintenanceExec(t, s.db, `INSERT INTO audit(id,action,at) VALUES('a2','boundary',2),('a3','today',?)`, today)
	for _, cutoff := range []int64{-1, 0, today, today + 1} {
		if _, err := s.CleanLibrary(context.Background(), "audit", cutoff); !errors.Is(err, ErrInvalidLibraryCleanup) {
			t.Fatalf("accepted cutoff %d", cutoff)
		}
	}
	n, err = s.CleanLibrary(context.Background(), "audit", 2)
	if err != nil || n != 1 {
		t.Fatalf("audit: %d %v", n, err)
	}
	stats, err = s.LibraryMaintenanceStatus()
	if err != nil || stats.Audit != 2 || stats.History != 1 {
		t.Fatalf("audit cleanup boundary: %+v %v", stats, err)
	}
	if _, err := s.CleanLibrary(context.Background(), "/arbitrary/log/path", 0); !errors.Is(err, ErrInvalidLibraryCleanup) {
		t.Fatal("arbitrary cleanup accepted")
	}
}

func TestLibraryMaintenanceRejectsConcurrentScan(t *testing.T) {
	s, _ := maintenanceTestService(t)
	data, _ := maintenanceExport(t, s)
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if err := s.RestoreLibrary(context.Background(), data); !errors.Is(err, ErrLibraryBusy) {
		t.Fatalf("restore during scan: %v", err)
	}
	for _, kind := range []string{"catalog", "orphan-covers"} {
		if _, err := s.CleanLibrary(context.Background(), kind, 0); !errors.Is(err, ErrLibraryBusy) {
			t.Fatalf("%s during scan: %v", kind, err)
		}
	}
}

func TestLibraryBackupValidationLimits(t *testing.T) {
	s, _ := maintenanceTestService(t)
	_, b := maintenanceExport(t, s)
	b.Tracks = make([]backupTrack, maxBackupRecords+1)
	if err := validateLibraryBackup(&b); !errors.Is(err, ErrInvalidLibraryBackup) {
		t.Fatal("record limit not enforced")
	}
	b.Tracks = []backupTrack{}
	data := make([]byte, maxBackupCoverBytes+1)
	hash := sha256.Sum256(data)
	b.Covers = []backupCover{{Hash: hex.EncodeToString(hash[:]), MIME: "image/png", Data: data}}
	if err := validateLibraryBackup(&b); !errors.Is(err, ErrInvalidLibraryBackup) {
		t.Fatal("cumulative cover size limit not enforced")
	}
}

func TestLibraryBackupExtendedTrackColumns(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	// A new schema is used only if the collaborating migration has landed.
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('tracks') WHERE name='composer'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Skip("extended schema not available")
	}
	maintenanceExec(t, s.db, `UPDATE tracks SET album_artist='album artist',composer='composer',year=1999,disc_number=2,track_number=7 WHERE id='t1'`)
	maintenanceExec(t, s.db, `UPDATE play_history SET track_id='t1' WHERE id='h1'`)
	data, b := maintenanceExport(t, s)
	if b.Tracks[0].Composer != "composer" || b.Tracks[0].Year != 1999 {
		t.Fatal("extended fields absent")
	}
	if err := s.RestoreLibrary(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	after, _ := maintenanceExport(t, s)
	if !bytes.Equal(data, after) {
		t.Fatal("extended fields lost during restore")
	}
}

func TestBackupPathValidation(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../mnt", "mnt/child", `mnt\child`, "C:drive", "mnt\n", "mnt.", "NUL", "COM1"} {
		if backupSourceID(id) {
			t.Errorf("accepted source ID %q", id)
		}
	}
	for _, p := range []string{"", ".", "..", "../a.wav", "a/../b.wav", "a/./b.wav", "/a.wav", "//nas/share/a.wav", `\\nas\share\a.wav`, "C:/a.wav", "C:a.wav", `a\b.wav`, "a//b.wav", "a/", "a\n.wav", "a\t.wav", "NUL.wav", "a:stream", "dir./a.wav"} {
		if backupRelPath(p) {
			t.Errorf("accepted relative path %q", p)
		}
	}
	for _, p := range []string{"song.wav", "Artist/Album/01 Track.flac", "艺术家/曲目.wav"} {
		if !backupRelPath(p) {
			t.Errorf("rejected safe relative path %q", p)
		}
	}
	root := t.TempDir()
	for _, p := range []string{"", "relative", root + string(filepath.Separator) + "..", root + "\t", root + "\x7f"} {
		if backupLocalTarget(p) {
			t.Errorf("accepted local target %q", p)
		}
	}
	if !backupLocalTarget(filepath.Join(root, "offline")) || !backupLocalTarget(filepath.ToSlash(root)) {
		t.Fatal("clean absolute targets must not require stat")
	}
	for _, change := range []func(*backupSource){
		func(v *backupSource) { v.ID = "../escape" },
		func(v *backupSource) { v.Host = "nas,evil" },
		func(v *backupSource) { v.Host = "." },
		func(v *backupSource) { v.Host = "nas\x01" },
		func(v *backupSource) { v.Share = ".." },
		func(v *backupSource) { v.Share = "share/path" },
		func(v *backupSource) { v.Version = "3.0,credentials=bad" },
	} {
		v := backupSource{ID: "m1", Kind: "smb", Host: "nas", Share: "music", Version: "3.0"}
		change(&v)
		if validateBackupSource(v) {
			t.Errorf("accepted invalid source: %+v", v)
		}
	}
}

func TestRestoreDerivesPathsAndAllowsOfflineLocalSources(t *testing.T) {
	s, _ := maintenanceTestService(t)
	_, b := maintenanceExport(t, s)
	offline := filepath.Join(t.TempDir(), "unavailable")
	b.Sources = []backupSource{
		{ID: "remote", Kind: "smb", Host: "safe-nas", Share: "music", Target: "/attacker/chosen/mountpoint"},
		{ID: "local", Kind: "local", Host: "localhost", Share: "Local music", Target: offline},
	}
	for _, v := range b.Sources {
		b.Tracks = append(b.Tracks, backupTrack{ID: "track-" + v.ID, MountID: v.ID, Path: "/arbitrary/private.wav", RelPath: "Album/song.wav", AddedAt: 17})
		b.TrackIdentities = append(b.TrackIdentities, backupTrackIdentity{ID: "track-" + v.ID, MountID: v.ID, RelPath: "Album/song.wav", AddedAt: 17})
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreLibrary(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	for _, v := range b.Sources {
		m, err := s.GetMount(v.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := offline
		if v.Kind == "smb" {
			want = filepath.Join(s.mountRoot, v.ID)
			if runtime.GOOS == "windows" {
				want = "//safe-nas/music"
			}
		}
		if m.Target != want || m.Status != StatusUnmounted || m.Username != "" || m.Password != "" || m.Domain != "" {
			t.Fatalf("unsafe restored source: %+v", m)
		}
		tr, err := s.GetTrack("track-" + v.ID)
		if err != nil || tr.Path != filepath.Join(want, "Album", "song.wav") {
			t.Fatalf("track path was not derived: %+v %v", tr, err)
		}
	}
	if _, err := os.Stat(offline); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore touched offline filesystem: %v", err)
	}
}

func TestRestoreRejectsChangedSourceIdentityBeforeWrites(t *testing.T) {
	for _, kind := range []string{"host", "share", "kind", "stored-smb-target", "local-target", "local-target-case"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := maintenanceTestService(t)
			maintenanceFixture(t, s)
			if strings.HasPrefix(kind, "local-") {
				maintenanceExec(t, s.db, `UPDATE mounts SET kind='local',host='localhost',version='',target=? WHERE id='m1'`, filepath.Join(t.TempDir(), "Music"))
			}
			_, b := maintenanceExport(t, s)
			switch kind {
			case "host":
				b.Sources[0].Host = "attacker-host"
			case "share":
				b.Sources[0].Share = "attacker-share"
			case "kind":
				b.Sources[0].Kind, b.Sources[0].Host, b.Sources[0].Version, b.Sources[0].Target = "local", "localhost", "", t.TempDir()
			case "stored-smb-target":
				maintenanceExec(t, s.db, `UPDATE mounts SET target=? WHERE id='m1'`, filepath.Join(t.TempDir(), "different"))
			case "local-target":
				b.Sources[0].Target = filepath.Join(t.TempDir(), "other")
			case "local-target-case":
				b.Sources[0].Target = filepath.Join(filepath.Dir(b.Sources[0].Target), "music")
			}
			before, _ := maintenanceExport(t, s)
			// If validation happens only after writes, this trigger changes the error.
			maintenanceExec(t, s.db, `CREATE TRIGGER no_restore_deletes BEFORE DELETE ON tracks BEGIN SELECT RAISE(ABORT,'unexpected delete'); END`)
			data, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreLibrary(t.Context(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
				t.Fatalf("identity change not rejected before writes: %v", err)
			}
			after, _ := maintenanceExport(t, s)
			if !bytes.Equal(before, after) {
				t.Fatal("identity rejection changed catalog")
			}
			m, err := s.GetMount("m1")
			if err != nil || m.Password != "test-nas-password" || m.Username != "test-nas-username" || m.Domain != "test-nas-domain" {
				t.Fatal("identity rejection changed credentials")
			}
		})
	}
}

func TestRestoreIgnoresSuppliedSMBTargetAndNormalizesIdentity(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	_, b := maintenanceExport(t, s)
	originalTarget := b.Sources[0].Target
	maintenanceExec(t, s.db, `UPDATE mounts SET target=? WHERE id='m1'`, filepath.ToSlash(originalTarget))
	b.Sources[0].Target = "/attacker/target"
	b.Tracks[0].Path = "/attacker/file"
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreLibrary(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMount("m1")
	tr, _ := s.GetTrack("t1")
	if m.Target != originalTarget || m.Password != "test-nas-password" || tr.Path != filepath.Join(originalTarget, "one.flac") {
		t.Fatalf("restore trusted supplied paths: %+v %+v", m, tr)
	}
}

func TestRestoreRequiresExplicitSMBUnmountIncludingOmittedSources(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained", true: "omitted"}[omit], func(t *testing.T) {
			s, _ := maintenanceTestService(t)
			maintenanceFixture(t, s)
			maintenanceExec(t, s.db, `UPDATE mounts SET status='mounted',mounted_at=10 WHERE id='m1'`)
			before, b := maintenanceExport(t, s)
			if omit {
				b.Sources, b.Tracks, b.TrackIdentities = []backupSource{}, []backupTrack{}, []backupTrackIdentity{}
			}
			data, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			maintenanceExec(t, s.db, `CREATE TRIGGER no_restore_deletes BEFORE DELETE ON tracks BEGIN SELECT RAISE(ABORT,'unexpected delete'); END`)
			if err := s.RestoreLibrary(t.Context(), data); !errors.Is(err, ErrLibraryBusy) {
				t.Fatalf("live SMB restore: %v", err)
			}
			after, _ := maintenanceExport(t, s)
			m, _ := s.GetMount("m1")
			if !bytes.Equal(before, after) || m.Status != StatusMounted || m.MountedAt != 10 {
				t.Fatal("busy restore modified source/catalog")
			}
			maintenanceExec(t, s.db, `DROP TRIGGER no_restore_deletes`)
			if _, err := s.Unmount(t.Context(), "m1"); err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreLibrary(t.Context(), data); err != nil {
				t.Fatalf("restore after explicit unmount: %v", err)
			}
		})
	}
}

func TestBackupRasterCoverValidation(t *testing.T) {
	s, _ := maintenanceTestService(t)
	_, b := maintenanceExport(t, s)
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var jpegData, gifData bytes.Buffer
	if err := jpeg.Encode(&jpegData, im, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifData, im, nil); err != nil {
		t.Fatal(err)
	}
	pngData := maintenancePNG(t, 50)
	webpData, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, mime string
		data       []byte
		valid      bool
	}{
		{"png", "image/png", pngData, true},
		{"jpeg", "image/jpeg", jpegData.Bytes(), true},
		{"gif", "image/gif", gifData.Bytes(), true},
		{"webp", "image/webp", webpData, true},
		{"webp-mismatch", "image/png", webpData, false},
		{"webp-truncated", "image/webp", webpData[:25], false},
		{"svg", "image/svg+xml", []byte(`<svg onload="alert(1)"/>`), false},
		{"html-as-png", "image/png", []byte(`<html><script>alert(1)</script></html>`), false},
		{"mismatch", "image/jpeg", pngData, false},
		{"truncated", "image/png", pngData[:8], false},
		{"unsupported", "image/bmp", pngData, false},
		{"invalid-webp", "image/webp", []byte("RIFFxxxxWEBPVP8 arbitrary"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			hash := sha256.Sum256(test.data)
			b.Covers = []backupCover{{Hash: hex.EncodeToString(hash[:]), MIME: test.mime, Data: test.data}}
			err := validateLibraryBackup(&b)
			if (err == nil) != test.valid {
				t.Fatalf("cover validity=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestCleanupScanRestoresPlaylistIdentityAndAddedTime(t *testing.T) {
	for _, portable := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-library", true: "portable-backup"}[portable], func(t *testing.T) {
			s, _ := maintenanceTestService(t)
			root := t.TempDir()
			buildWAV(t, filepath.Join(root, "song.wav"))
			m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root, Share: "Music"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Mount(t.Context(), m.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Scan(t.Context(), m.ID); err != nil {
				t.Fatal(err)
			}
			tracks, err := s.ListTracks(m.ID)
			if err != nil || len(tracks) != 1 {
				t.Fatalf("tracks: %+v %v", tracks, err)
			}
			tr := tracks[0]
			tr.AddedAt = 123
			maintenanceExec(t, s.db, `UPDATE tracks SET added_at=? WHERE id=?`, tr.AddedAt, tr.ID)
			p, err := s.CreatePlaylist("Favorites")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.AddPlaylistItems(p.ID, []string{tr.ID}); err != nil {
				t.Fatal(err)
			}
			maintenanceExec(t, s.db, `INSERT INTO play_history(id,node_id,track_id,started_at,title) VALUES('h','n',?,1,'Frozen history')`, tr.ID)
			if n, err := s.CleanLibrary(t.Context(), "catalog", 0); err != nil || n != 1 {
				t.Fatalf("cleanup: %d %v", n, err)
			}
			detail, err := s.GetPlaylist(p.ID)
			if err != nil || len(detail.Items) != 1 || !detail.Items[0].Missing {
				t.Fatalf("cleaned playlist: %+v %v", detail, err)
			}
			if portable {
				data, b := maintenanceExport(t, s)
				if len(b.Tracks) != 0 || len(b.TrackIdentities) != 1 || b.TrackIdentities[0].ID != tr.ID {
					t.Fatal("cleanup backup lost identity")
				}
				// A distinct destination may contain stale identities. Restore must
				// replace them atomically rather than merge conflicting IDs/paths.
				s, _ = maintenanceTestService(t)
				maintenanceExec(t, s.db, `INSERT INTO mounts(id,kind,host,share,target,version,created_at) VALUES(?,'local','localhost','Music',?,'',1)`, m.ID, root)
				maintenanceExec(t, s.db, `INSERT INTO track_identities(id,mount_id,rel_path,added_at) VALUES('stale',?,'song.wav',999),('extra',?,'extra.wav',999)`, m.ID, m.ID)
				if err := s.RestoreLibrary(t.Context(), data); err != nil {
					t.Fatal(err)
				}
				var n int
				if err := s.db.QueryRow(`SELECT COUNT(*) FROM track_identities`).Scan(&n); err != nil || n != 1 {
					t.Fatalf("stale identities retained: %d %v", n, err)
				}
				if _, err := s.Mount(t.Context(), m.ID); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := s.Scan(t.Context(), m.ID); err != nil || n != 1 {
				t.Fatalf("recovery scan: %d %v", n, err)
			}
			got, err := s.GetTrack(tr.ID)
			if err != nil || got.AddedAt != tr.AddedAt || got.RelPath != tr.RelPath {
				t.Fatalf("identity/time lost: %+v %v", got, err)
			}
			detail, err = s.GetPlaylist(p.ID)
			if err != nil || len(detail.Items) != 1 || detail.Items[0].Missing || detail.Items[0].TrackID != tr.ID {
				t.Fatalf("playlist not recovered: %+v %v", detail, err)
			}
			var title string
			if err := s.db.QueryRow(`SELECT title FROM play_history WHERE id='h'`).Scan(&title); err != nil || title != "Frozen history" {
				t.Fatal("history changed on identity recovery")
			}
			if err := s.DeleteMount(t.Context(), m.ID); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM track_identities`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("source deletion kept identities: %d %v", count, err)
			}
		})
	}
}

func TestCatalogCleanupIdentitySnapshotRollsBack(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	before, _ := maintenanceExport(t, s)
	maintenanceExec(t, s.db, `CREATE TRIGGER fail_catalog_cleanup BEFORE DELETE ON tracks BEGIN SELECT RAISE(ABORT,'failed cleanup'); END`)
	if _, err := s.CleanLibrary(t.Context(), "catalog", 0); !errors.Is(err, ErrLibraryMaintenance) {
		t.Fatalf("cleanup: %v", err)
	}
	after, _ := maintenanceExport(t, s)
	if !bytes.Equal(before, after) {
		t.Fatal("failed cleanup modified catalog")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM track_identities`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("identity snapshot survived rollback: %d %v", count, err)
	}
}

func TestBackupIdentityValidation(t *testing.T) {
	s, _ := maintenanceTestService(t)
	maintenanceFixture(t, s)
	original, _ := maintenanceExport(t, s)
	for _, test := range []struct {
		name   string
		change func(*libraryBackup)
	}{
		{"required", func(b *libraryBackup) { b.TrackIdentities = nil }},
		{"missing-current", func(b *libraryBackup) { b.TrackIdentities = []backupTrackIdentity{} }},
		{"unknown-source", func(b *libraryBackup) { b.TrackIdentities[0].MountID = "unknown" }},
		{"duplicate-id", func(b *libraryBackup) {
			v := b.TrackIdentities[0]
			v.RelPath = "other.wav"
			b.TrackIdentities = append(b.TrackIdentities, v)
		}},
		{"duplicate-path", func(b *libraryBackup) {
			v := b.TrackIdentities[0]
			v.ID = "other-id"
			b.TrackIdentities = append(b.TrackIdentities, v)
		}},
		{"id-mismatch", func(b *libraryBackup) { b.TrackIdentities[0].ID = "other-id" }},
		{"time-mismatch", func(b *libraryBackup) { b.TrackIdentities[0].AddedAt++ }},
		{"negative-time", func(b *libraryBackup) { b.TrackIdentities[0].AddedAt = -1 }},
		{"traversal", func(b *libraryBackup) {
			b.TrackIdentities[0].RelPath = "../one.flac"
			b.Tracks[0].RelPath = "../one.flac"
		}},
		{"unc", func(b *libraryBackup) {
			b.TrackIdentities[0].RelPath = `\\host\share\one.flac`
			b.Tracks[0].RelPath = b.TrackIdentities[0].RelPath
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var b libraryBackup
			if err := json.Unmarshal(original, &b); err != nil {
				t.Fatal(err)
			}
			test.change(&b)
			data, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreLibrary(t.Context(), data); !errors.Is(err, ErrInvalidLibraryBackup) {
				t.Fatalf("accepted inconsistent identities: %v", err)
			}
			after, _ := maintenanceExport(t, s)
			if !bytes.Equal(after, original) {
				t.Fatal("invalid identity restore changed catalog")
			}
		})
	}
}
