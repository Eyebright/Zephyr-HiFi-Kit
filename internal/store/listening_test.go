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
