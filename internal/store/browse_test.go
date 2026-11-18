package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBrowseAdditiveMigrationPreservesFrozenHistory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	// The old schema has none of the new descriptive columns or source kind.
	_, err = db.Exec(`INSERT INTO mounts(id,host,share,target,created_at) VALUES('m','nas','music','/music',1);
		INSERT INTO tracks(id,mount_id,path,rel_path,title,artist,album,scanned_at) VALUES('t','m','/music/a.wav','a.wav','Changed title','Changed artist','Changed album',2);
		ALTER TABLE play_history ADD COLUMN title TEXT NOT NULL DEFAULT '';
		ALTER TABLE play_history ADD COLUMN artist TEXT NOT NULL DEFAULT '';
		ALTER TABLE play_history ADD COLUMN album TEXT NOT NULL DEFAULT '';
		ALTER TABLE play_history ADD COLUMN genre TEXT NOT NULL DEFAULT '';
		ALTER TABLE play_history ADD COLUMN cover_hash TEXT NOT NULL DEFAULT '';
		ALTER TABLE play_history ADD COLUMN measured INTEGER NOT NULL DEFAULT 0;
		INSERT INTO play_history(id,node_id,track_id,started_at,title) VALUES('h','n','t',1,'Frozen');
		INSERT INTO server_state(key,value) VALUES('listening_history_snapshot_v1','1');`)
	if err != nil {
		t.Fatal(err)
	}
	before := readHistorySnapshots(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		var albumArtist, composer, kind string
		var year, disc, track int
		err = s.DB.QueryRow(`SELECT album_artist,composer,year,disc_number,track_number FROM tracks WHERE id='t'`).Scan(&albumArtist, &composer, &year, &disc, &track)
		if err != nil || albumArtist != "" || composer != "" || year != 0 || disc != 0 || track != 0 {
			t.Fatalf("legacy defaults: %q %q %d %d %d %v", albumArtist, composer, year, disc, track, err)
		}
		if err := s.DB.QueryRow(`SELECT kind FROM mounts WHERE id='m'`).Scan(&kind); err != nil || kind != "smb" {
			t.Fatalf("legacy source kind: %q %v", kind, err)
		}
		if got := readHistorySnapshots(t, s.DB); !reflect.DeepEqual(got, before) {
			t.Fatalf("frozen history changed: got=%+v before=%+v", got, before)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrackIdentityAdditiveMigrationAndConstraints(t *testing.T) {
	p := filepath.Join(t.TempDir(), "identity-migration.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DROP TABLE track_identities;
		INSERT INTO mounts(id,host,share,target,created_at) VALUES('m','nas','music','/music',1);
		INSERT INTO play_history(id,node_id,track_id,started_at,title) VALUES('h','n','t',1,'Frozen')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err = Open(p)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := s.DB.Exec(`INSERT INTO track_identities(id,mount_id,rel_path,added_at) VALUES('t','m','a.wav',17)`); err != nil {
				t.Fatal(err)
			}
		}
		var added int64
		if err := s.DB.QueryRow(`SELECT added_at FROM track_identities WHERE id='t'`).Scan(&added); err != nil || added != 17 {
			t.Fatalf("identity after migration: %d %v", added, err)
		}
		for _, query := range []string{
			`INSERT INTO track_identities VALUES('t','m','b.wav',17)`,
			`INSERT INTO track_identities VALUES('other','m','a.wav',17)`,
			`INSERT INTO track_identities VALUES('other','missing','b.wav',17)`,
			`INSERT INTO track_identities VALUES('other','m','b.wav',-1)`,
		} {
			if _, err := s.DB.Exec(query); err == nil {
				t.Fatalf("identity constraint not enforced: %s", query)
			}
		}
		if i == 1 {
			if _, err := s.DB.Exec(`DELETE FROM mounts WHERE id='m'`); err != nil {
				t.Fatal(err)
			}
			var identities, history int
			if err := s.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM track_identities),(SELECT COUNT(*) FROM play_history)`).Scan(&identities, &history); err != nil || identities != 0 || history != 1 {
				t.Fatalf("cascade/history: %d %d %v", identities, history, err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
