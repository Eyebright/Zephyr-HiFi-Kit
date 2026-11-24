package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type historySnapshot struct {
	Title, Artist, Album, Genre, Cover string
	Measured                           bool
}

// Build an old database directly, without Open (which would set the marker).
func listeningHistoryFixture(t *testing.T, withSnapshots bool) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE tracks (
			id TEXT PRIMARY KEY, mount_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '',
			album TEXT NOT NULL DEFAULT '', cover_hash TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE play_history (
			id TEXT PRIMARY KEY, node_id TEXT NOT NULL, track_id TEXT NOT NULL,
			started_at INTEGER NOT NULL
		);
		CREATE TABLE server_state (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
		INSERT INTO server_state VALUES('unrelated', 'keep');
		INSERT INTO tracks VALUES('t1', 'm', 'Library title', 'Library artist', 'Library album', 'library-cover', 60000);
		INSERT INTO play_history VALUES('legacy', 'n', 't1', 100), ('missing', 'n', 'gone', 100);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if withSnapshots {
		_, err = db.Exec(`
			ALTER TABLE tracks ADD COLUMN genre TEXT NOT NULL DEFAULT '';
			UPDATE tracks SET genre='Jazz';
			ALTER TABLE play_history ADD COLUMN title TEXT NOT NULL DEFAULT '';
			ALTER TABLE play_history ADD COLUMN artist TEXT NOT NULL DEFAULT '';
			ALTER TABLE play_history ADD COLUMN album TEXT NOT NULL DEFAULT '';
			ALTER TABLE play_history ADD COLUMN genre TEXT NOT NULL DEFAULT '';
			ALTER TABLE play_history ADD COLUMN cover_hash TEXT NOT NULL DEFAULT '';
			ALTER TABLE play_history ADD COLUMN measured INTEGER NOT NULL DEFAULT 0;
			INSERT INTO play_history(id,node_id,track_id,started_at,title,artist,album,genre,cover_hash,measured) VALUES
				('partial','n','t1',100,'Saved title','','Saved album','','',0),
				('saved','n','t1',100,'Saved title','Saved artist','Saved album','Saved genre','saved-cover',0),
				('measured','n','t1',100,'Measured title','Measured artist','Measured album','Measured genre','measured-cover',1),
				('measured-empty','n','t1',100,'','','','','',1),
				('missing-partial','n','gone',100,'','Saved artist','','','',0);
			CREATE TABLE listen_daily (
				history_id TEXT NOT NULL, day INTEGER NOT NULL, listen_ms INTEGER NOT NULL DEFAULT 0,
				last_played_at INTEGER NOT NULL, PRIMARY KEY(history_id,day)
			);
			INSERT INTO listen_daily VALUES('measured', 0, 4321, 101);
		`)
		if err != nil {
			t.Fatal(err)
		}
	}
	return path, db
}

func readHistorySnapshots(t *testing.T, db *sql.DB) map[string]historySnapshot {
	t.Helper()
	rows, err := db.Query(`SELECT id,title,artist,album,genre,cover_hash,measured FROM play_history`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[string]historySnapshot{}
	for rows.Next() {
		var id string
		var snapshot historySnapshot
		if err := rows.Scan(&id, &snapshot.Title, &snapshot.Artist, &snapshot.Album, &snapshot.Genre, &snapshot.Cover, &snapshot.Measured); err != nil {
			t.Fatal(err)
		}
		result[id] = snapshot
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestListeningHistorySnapshotMigration(t *testing.T) {
	for _, withSnapshots := range []bool{false, true} {
		name := "original-schema"
		if withSnapshots {
			name = "existing-snapshots"
		}
		t.Run(name, func(t *testing.T) {
			path, db := listeningHistoryFixture(t, withSnapshots)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			st, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			legacy := historySnapshot{Title: "Library title", Artist: "Library artist", Album: "Library album", Cover: "library-cover"}
			want := map[string]historySnapshot{"legacy": legacy, "missing": {}}
			if withSnapshots {
				legacy.Genre = "Jazz"
				want["legacy"] = legacy
				want["partial"] = historySnapshot{Title: "Saved title", Artist: "Library artist", Album: "Saved album", Genre: "Jazz", Cover: "library-cover"}
				want["saved"] = historySnapshot{Title: "Saved title", Artist: "Saved artist", Album: "Saved album", Genre: "Saved genre", Cover: "saved-cover"}
				want["measured"] = historySnapshot{Title: "Measured title", Artist: "Measured artist", Album: "Measured album", Genre: "Measured genre", Cover: "measured-cover", Measured: true}
				want["measured-empty"] = historySnapshot{Measured: true}
				want["missing-partial"] = historySnapshot{Artist: "Saved artist"}
			}
			check := func() {
				t.Helper()
				if got := readHistorySnapshots(t, st.DB); !reflect.DeepEqual(got, want) {
					t.Fatalf("snapshots=%+v, want %+v", got, want)
				}
				var marker, unrelated string
				if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key='listening_history_snapshot_v1'`).Scan(&marker); err != nil || marker != "1" {
					t.Fatalf("marker=%q err=%v", marker, err)
				}
				if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key='unrelated'`).Scan(&unrelated); err != nil || unrelated != "keep" {
					t.Fatalf("unrelated state=%q err=%v", unrelated, err)
				}
				var count int
				var ms int64
				if err := st.DB.QueryRow(`SELECT COUNT(*),COALESCE(SUM(listen_ms),0) FROM listen_daily`).Scan(&count, &ms); err != nil {
					t.Fatal(err)
				}
				if (!withSnapshots && (count != 0 || ms != 0)) || (withSnapshots && (count != 1 || ms != 4321)) {
					t.Fatalf("migration changed or fabricated duration: count=%d ms=%d", count, ms)
				}
			}
			check()
			for _, change := range []string{
				`UPDATE tracks SET title='Retagged',artist='Retagged',album='Retagged',genre='Retagged',cover_hash='retagged-cover';
				 INSERT INTO tracks(id,mount_id,title,artist,album,genre,cover_hash) VALUES('gone','m','New title','New artist','New album','New genre','new-cover');
				 INSERT INTO play_history(id,node_id,track_id,started_at) VALUES('late-legacy','n','t1',200)`,
				`DELETE FROM tracks`,
			} {
				if _, err := st.DB.Exec(change); err != nil {
					t.Fatal(err)
				}
				want["late-legacy"] = historySnapshot{}
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				st, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				check()
			}
		})
	}
}

func playbackDeviceStateFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy-devices.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE nodes (
			node_id TEXT PRIMARY KEY, alias TEXT, node_group TEXT, tags TEXT, arch TEXT,
			distro TEXT, kernel TEXT, status TEXT NOT NULL DEFAULT 'unknown', last_seen INTEGER,
			bom_id TEXT, bom_version INTEGER, agent_version TEXT, playing INTEGER NOT NULL DEFAULT 0,
			active_proto TEXT, dac_holder_pid INTEGER, cert_fingerprint TEXT, registered_at INTEGER NOT NULL,
			dac_device TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE node_queue (
			node_id TEXT PRIMARY KEY, track_ids TEXT NOT NULL DEFAULT '[]',
			position INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE node_sleep_timer (node_id TEXT PRIMARY KEY, stop_at INTEGER NOT NULL);
		CREATE TABLE server_state (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
		INSERT INTO server_state VALUES('listening_history_snapshot_v1','1'),('unrelated','keep');
		INSERT INTO nodes(node_id,dac_device,registered_at) VALUES
			('n-pref','hw:1,0',1),('n-default','',2),('n-none','hw:9,9',3);
		INSERT INTO node_queue VALUES
			('n-pref','["t1","t2"]',1,11),('n-default','["t3"]',0,12),('n-missing','["t4"]',0,13);
		INSERT INTO node_sleep_timer VALUES('n-pref',101),('n-default',102),('n-missing',103);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestPlaybackDeviceStateMigration(t *testing.T) {
	path, db := playbackDeviceStateFixture(t)
	st := &Store{DB: db}
	if err := st.migrate(); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		rows, err := st.DB.Query(`SELECT device_key,track_ids,position,updated_at,mode,shuffle_order
			FROM playback_queue ORDER BY device_key`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]string{}
		for rows.Next() {
			var key, ids, mode, order string
			var pos, updated int
			if err := rows.Scan(&key, &ids, &pos, &updated, &mode, &order); err != nil {
				t.Fatal(err)
			}
			got[key] = ids
			if mode != "sequence" || order != "[]" {
				t.Fatalf("queue defaults wrong for %s: mode=%s order=%s", key, mode, order)
			}
			if key == "n-pref::hw:1,0" && (ids != `["t1","t2"]` || pos != 1 || updated != 11) {
				t.Fatalf("preferred queue migration wrong: %s %d %d", ids, pos, updated)
			}
			if key == "n-default::default" && (ids != `["t3"]` || pos != 0 || updated != 12) {
				t.Fatalf("default queue migration wrong: %s %d %d", ids, pos, updated)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got["n-pref::hw:1,0"] == "" || got["n-default::default"] == "" {
			t.Fatalf("migrated queues = %+v", got)
		}
		timers, err := st.DB.Query(`SELECT device_key,stop_at FROM playback_sleep_timer ORDER BY device_key`)
		if err != nil {
			t.Fatal(err)
		}
		defer timers.Close()
		gotTimers := map[string]int64{}
		for timers.Next() {
			var key string
			var stopAt int64
			if err := timers.Scan(&key, &stopAt); err != nil {
				t.Fatal(err)
			}
			gotTimers[key] = stopAt
		}
		if err := timers.Err(); err != nil {
			t.Fatal(err)
		}
		if len(gotTimers) != 2 || gotTimers["n-pref::hw:1,0"] != 101 || gotTimers["n-default::default"] != 102 {
			t.Fatalf("migrated timers = %+v", gotTimers)
		}
		var marker, unrelated string
		if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key='playback_device_state_v1'`).Scan(&marker); err != nil || marker != "1" {
			t.Fatalf("marker=%q err=%v", marker, err)
		}
		if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key='unrelated'`).Scan(&unrelated); err != nil || unrelated != "keep" {
			t.Fatalf("unrelated state=%q err=%v", unrelated, err)
		}
	}
	check()
	if _, err := st.DB.Exec(`UPDATE playback_queue SET track_ids='["edited"]' WHERE device_key='n-pref::hw:1,0';
		UPDATE playback_sleep_timer SET stop_at=999 WHERE device_key='n-pref::hw:1,0';
		UPDATE node_queue SET track_ids='["legacy-changed"]' WHERE node_id='n-pref';
		UPDATE node_sleep_timer SET stop_at=888 WHERE node_id='n-pref'`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrate(); err != nil {
		t.Fatal(err)
	}
	var ids string
	var stopAt int64
	if err := st.DB.QueryRow(`SELECT track_ids FROM playback_queue WHERE device_key='n-pref::hw:1,0'`).Scan(&ids); err != nil || ids != `["edited"]` {
		t.Fatalf("repeat migration overwrote queue: %q %v", ids, err)
	}
	if err := st.DB.QueryRow(`SELECT stop_at FROM playback_sleep_timer WHERE device_key='n-pref::hw:1,0'`).Scan(&stopAt); err != nil || stopAt != 999 {
		t.Fatalf("repeat migration overwrote timer: %d %v", stopAt, err)
	}
	if _, err := st.DB.Exec(`DELETE FROM playback_queue; DELETE FROM playback_sleep_timer`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, table := range []string{"playback_queue", "playback_sleep_timer"} {
		var count int
		if err := st.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("reopen resurrected %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestPlaybackDeviceStateMigrationRollback(t *testing.T) {
	_, db := playbackDeviceStateFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_device_marker BEFORE INSERT ON server_state
		WHEN NEW.key='playback_device_state_v1'
		BEGIN SELECT RAISE(ABORT, 'device marker failure'); END`); err != nil {
		t.Fatal(err)
	}
	st := &Store{DB: db}
	if err := st.migrate(); err == nil || !strings.Contains(err.Error(), "device marker failure") {
		t.Fatalf("expected marker failure, got %v", err)
	}
	for _, table := range []string{"playback_queue", "playback_sleep_timer"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed migration left %s rows: count=%d err=%v", table, count, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_state WHERE key='playback_device_state_v1'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left marker: count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_device_marker`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrate(); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM playback_queue WHERE device_key='n-pref::hw:1,0'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry did not migrate preferred queue: count=%d err=%v", count, err)
	}
}

func TestListeningHistorySnapshotMigrationRollback(t *testing.T) {
	_, db := listeningHistoryFixture(t, true)
	before := readHistorySnapshots(t, db)
	if _, err := db.Exec(`CREATE TRIGGER reject_snapshot_marker BEFORE INSERT ON server_state
		WHEN NEW.key='listening_history_snapshot_v1'
		BEGIN SELECT RAISE(ABORT, 'snapshot marker failure'); END`); err != nil {
		t.Fatal(err)
	}
	st := &Store{DB: db}
	if err := st.migrate(); err == nil || !strings.Contains(err.Error(), "snapshot marker failure") {
		t.Fatalf("expected marker failure, got %v", err)
	}
	if got := readHistorySnapshots(t, db); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed migration did not roll back: got %+v, want %+v", got, before)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_state WHERE key='listening_history_snapshot_v1'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left marker: count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_snapshot_marker`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrate(); err != nil {
		t.Fatal(err)
	}
	if got := readHistorySnapshots(t, db)["legacy"]; got.Title != "Library title" || got.Measured {
		t.Fatalf("migration could not retry after rollback: %+v", got)
	}
	// Once committed, even the backfill itself must not consult the catalog again.
	if _, err := db.Exec(`ALTER TABLE tracks RENAME TO unavailable_tracks`); err != nil {
		t.Fatal(err)
	}
	if err := st.backfillListeningSnapshots(); err != nil {
		t.Fatalf("completed migration queried tracks again: %v", err)
	}
}
