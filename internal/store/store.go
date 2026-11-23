// Package store opens the SQLite database and applies the schema.
// It uses the pure-Go modernc.org/sqlite driver (no CGO), so the server
// cross-compiles cleanly to linux/amd64 and linux/arm64.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps the database handle.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and migrates it.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is a single-writer engine; serialize access to avoid SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(schema); err != nil {
		return err
	}
	return s.additiveMigrate()
}

// additiveMigrate adds columns introduced after the initial schema to existing
// databases. Each ALTER is guarded by a PRAGMA table_info check so re-running
// migration on an up-to-date database is a no-op (idempotent, additive only).
func (s *Store) additiveMigrate() error {
	type col struct{ table, name, decl string }
	cols := []col{
		// nodes: management + licensing + OS-update + console fields (FR-3/4/6).
		{"nodes", "build_flavor", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "note", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "ip", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "authorized", "INTEGER NOT NULL DEFAULT 0"},
		{"nodes", "authorized_at", "INTEGER NOT NULL DEFAULT 0"},
		{"nodes", "play_protect", "INTEGER NOT NULL DEFAULT 1"},
		{"nodes", "auto_os_update", "INTEGER NOT NULL DEFAULT 0"},
		{"nodes", "os_window", "TEXT NOT NULL DEFAULT '23:00–07:00'"},
		{"nodes", "offline_after", "INTEGER NOT NULL DEFAULT 90"},
		{"nodes", "dac_arbitration", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "dac_device", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "secret", "TEXT NOT NULL DEFAULT ''"},
		{"nodes", "cpu", "REAL NOT NULL DEFAULT 0"},
		{"nodes", "mem_mb", "REAL NOT NULL DEFAULT 0"},
		{"nodes", "temp_c", "REAL NOT NULL DEFAULT 0"},
		// alerts: lifecycle status + ack/resolve timestamps (FR-7.1).
		{"alerts", "kind", "TEXT NOT NULL DEFAULT ''"},
		{"alerts", "status", "TEXT NOT NULL DEFAULT 'open'"},
		{"alerts", "ack_at", "INTEGER NOT NULL DEFAULT 0"},
		{"alerts", "resolved_at", "INTEGER NOT NULL DEFAULT 0"},
		// audit: human detail + duration (FR-7.2).
		{"audit", "detail", "TEXT NOT NULL DEFAULT ''"},
		{"audit", "duration_ms", "INTEGER NOT NULL DEFAULT 0"},
		// tracks: embedded album art reference (covers table, sha256-keyed).
		{"tracks", "cover_hash", "TEXT NOT NULL DEFAULT ''"},
		{"tracks", "genre", "TEXT NOT NULL DEFAULT ''"},
		{"tracks", "added_at", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "album_artist", "TEXT NOT NULL DEFAULT ''"},
		{"tracks", "composer", "TEXT NOT NULL DEFAULT ''"},
		{"tracks", "year", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "disc_number", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "track_number", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "mtime_ns", "INTEGER NOT NULL DEFAULT 0"},
		// tracks: MQA marker (tagged or sample-analysis detected).
		{"tracks", "mqa", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "mqa_checked", "INTEGER NOT NULL DEFAULT 0"},
		// tracks: CUE-split virtual tracks reference a sample window in the image.
		{"tracks", "cue_start", "INTEGER NOT NULL DEFAULT 0"},
		{"tracks", "cue_end", "INTEGER NOT NULL DEFAULT 0"},
		{"mounts", "kind", "TEXT NOT NULL DEFAULT 'smb'"},
		{"mounts", "text_encoding", "TEXT NOT NULL DEFAULT 'auto'"},
		{"mounts", "last_scan", "INTEGER NOT NULL DEFAULT 0"},
		{"mounts", "scan_result", "TEXT NOT NULL DEFAULT '{}'"},
		{"play_history", "title", "TEXT NOT NULL DEFAULT ''"},
		{"play_history", "artist", "TEXT NOT NULL DEFAULT ''"},
		{"play_history", "album", "TEXT NOT NULL DEFAULT ''"},
		{"play_history", "genre", "TEXT NOT NULL DEFAULT ''"},
		{"play_history", "cover_hash", "TEXT NOT NULL DEFAULT ''"},
		{"play_history", "measured", "INTEGER NOT NULL DEFAULT 0"},
		// app_versions: distro package name for format=apt entries.
		{"app_versions", "apt_package", "TEXT NOT NULL DEFAULT ''"},
		// apps: how the artifact is obtained (distro | vendor | self).
		{"apps", "origin", "TEXT NOT NULL DEFAULT 'self'"},
		// node_queue: per-terminal playback mode + frozen shuffle order.
		{"node_queue", "mode", "TEXT NOT NULL DEFAULT 'sequence'"},
		{"node_queue", "shuffle_order", "TEXT NOT NULL DEFAULT '[]'"},
	}
	for _, c := range cols {
		if err := s.addColumnIfMissing(c.table, c.name, c.decl); err != nil {
			return fmt.Errorf("add column %s.%s: %w", c.table, c.name, err)
		}
	}
	return s.backfillListeningSnapshots()
}

// Freeze legacy metadata once, so future catalog changes cannot rewrite history.
func (s *Store) backfillListeningSnapshots() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin listening snapshot backfill: %w", err)
	}
	defer tx.Rollback()
	const marker = "listening_history_snapshot_v1"
	var done bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM server_state WHERE key=?)`, marker).Scan(&done); err != nil {
		return fmt.Errorf("check listening snapshot backfill: %w", err)
	}
	if done {
		return nil
	}
	if _, err := tx.Exec(`UPDATE play_history AS h SET
		title=COALESCE(NULLIF(h.title,''),(SELECT t.title FROM tracks t WHERE t.id=h.track_id),''),
		artist=COALESCE(NULLIF(h.artist,''),(SELECT t.artist FROM tracks t WHERE t.id=h.track_id),''),
		album=COALESCE(NULLIF(h.album,''),(SELECT t.album FROM tracks t WHERE t.id=h.track_id),''),
		genre=COALESCE(NULLIF(h.genre,''),(SELECT t.genre FROM tracks t WHERE t.id=h.track_id),''),
		cover_hash=COALESCE(NULLIF(h.cover_hash,''),(SELECT t.cover_hash FROM tracks t WHERE t.id=h.track_id),'')
		WHERE h.measured=0 AND EXISTS(SELECT 1 FROM tracks t WHERE t.id=h.track_id)
		AND (h.title='' OR h.artist='' OR h.album='' OR h.genre='' OR h.cover_hash='')`); err != nil {
		return fmt.Errorf("backfill listening snapshots: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO server_state(key,value) VALUES(?, '1')`, marker); err != nil {
		return fmt.Errorf("mark listening snapshot backfill: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit listening snapshot backfill: %w", err)
	}
	return nil
}

// addColumnIfMissing runs ALTER TABLE ADD COLUMN only when the column is absent.
func (s *Store) addColumnIfMissing(table, name, decl string) error {
	rows, err := s.DB.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var colName, colType string
		var dflt any
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if colName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.DB.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + decl)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS apps (
  app_id              TEXT PRIMARY KEY,
  name                TEXT NOT NULL,
  category            TEXT NOT NULL,
  origin              TEXT NOT NULL DEFAULT 'self',
  upstream            TEXT,
  license             TEXT,
  maintainer_state    TEXT,
  self_build_required INTEGER NOT NULL DEFAULT 0,
  default_channel     TEXT,
  created_at          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS app_versions (
  id                 TEXT PRIMARY KEY,
  app_id             TEXT NOT NULL REFERENCES apps(app_id),
  version            TEXT NOT NULL,
  arch               TEXT NOT NULL,
  format             TEXT,
  artifact_path      TEXT,
  size               INTEGER NOT NULL DEFAULT 0,
  sha256             TEXT,
  signature          TEXT,
  signed             INTEGER NOT NULL DEFAULT 0,
  build_flags        TEXT,
  capabilities       TEXT,
  distro_support     TEXT,
  glibc_min          TEXT,
  units              TEXT,
  ports              TEXT,
  config_template_id TEXT,
  deps_apps          TEXT,
  deps_pkgs          TEXT,
  occupies_dac       INTEGER NOT NULL DEFAULT 0,
  build_commit       TEXT,
  airplay2_enabled   INTEGER NOT NULL DEFAULT 0,
  qplay_ver          INTEGER NOT NULL DEFAULT 0,
  patched            INTEGER NOT NULL DEFAULT 0,
  banned             INTEGER NOT NULL DEFAULT 0,
  lifecycle          TEXT NOT NULL DEFAULT 'available',
  build_time         INTEGER,
  published_at       INTEGER,
  UNIQUE(app_id, version, arch)
);
CREATE INDEX IF NOT EXISTS idx_versions_app ON app_versions(app_id);

CREATE TABLE IF NOT EXISTS boms (
  bom_id           TEXT NOT NULL,
  version          INTEGER NOT NULL,
  role             TEXT,
  apps             TEXT,
  dep_closure      TEXT,
  config_templates TEXT,
  variables        TEXT,
  units_enabled    TEXT,
  aux_actions      TEXT,
  status           TEXT NOT NULL DEFAULT 'draft',
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  PRIMARY KEY (bom_id, version)
);

CREATE TABLE IF NOT EXISTS nodes (
  node_id          TEXT PRIMARY KEY,
  alias            TEXT,
  node_group       TEXT,
  tags             TEXT,
  arch             TEXT,
  distro           TEXT,
  kernel           TEXT,
  status           TEXT NOT NULL DEFAULT 'unknown',
  last_seen        INTEGER,
  bom_id           TEXT,
  bom_version      INTEGER,
  agent_version    TEXT,
  build_flavor     TEXT NOT NULL DEFAULT '',
  playing          INTEGER NOT NULL DEFAULT 0,
  active_proto     TEXT,
  dac_holder_pid   INTEGER,
  cert_fingerprint TEXT,
  registered_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS installed_apps (
  node_id     TEXT NOT NULL,
  app_id      TEXT NOT NULL,
  version     TEXT,
  package     TEXT,
  unit        TEXT,
  enabled     INTEGER NOT NULL DEFAULT 0,
  config_hash TEXT,
  build_flags TEXT,
  source      TEXT,
  PRIMARY KEY (node_id, app_id)
);

CREATE TABLE IF NOT EXISTS drifts (
  node_id  TEXT NOT NULL,
  app_id   TEXT NOT NULL,
  kind     TEXT NOT NULL,
  detail   TEXT,
  first_at INTEGER NOT NULL,
  PRIMARY KEY (node_id, app_id, kind)
);

CREATE TABLE IF NOT EXISTS jobs (
  id          TEXT PRIMARY KEY,
  node_id     TEXT NOT NULL,
  bom_id      TEXT,
  kind        TEXT NOT NULL,
  state       TEXT NOT NULL,
  phases      TEXT,
  reason      TEXT,
  created_at  INTEGER NOT NULL,
  started_at  INTEGER,
  finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_jobs_node ON jobs(node_id);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);

CREATE TABLE IF NOT EXISTS sessions (
  id            TEXT PRIMARY KEY,
  node_id       TEXT NOT NULL,
  proto         TEXT NOT NULL,
  source_client TEXT,
  state         TEXT,
  format        TEXT,
  volume        INTEGER,
  dac_device    TEXT,
  started_at    INTEGER NOT NULL,
  ended_at      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_node ON sessions(node_id);

CREATE TABLE IF NOT EXISTS alerts (
  id       TEXT PRIMARY KEY,
  rule     TEXT NOT NULL,
  severity TEXT NOT NULL,
  node_id  TEXT,
  message  TEXT,
  first_at INTEGER NOT NULL,
  last_at  INTEGER NOT NULL,
  count    INTEGER NOT NULL DEFAULT 1,
  acked_by TEXT
);

CREATE TABLE IF NOT EXISTS audit (
  id     TEXT PRIMARY KEY,
  actor  TEXT,
  ip     TEXT,
  action TEXT NOT NULL,
  target TEXT,
  params TEXT,
  result TEXT,
  reason TEXT,
  at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at);

CREATE TABLE IF NOT EXISTS build_records (
  id           TEXT PRIMARY KEY,
  app_id       TEXT NOT NULL,
  version      TEXT,
  arch         TEXT,
  strategy     TEXT,
  source_hash  TEXT,
  patch_hashes TEXT,
  build_flags  TEXT,
  dep_manifest TEXT,
  build_host   TEXT,
  output_hash  TEXT,
  state        TEXT NOT NULL DEFAULT 'queued',
  log_path     TEXT,
  started_at   INTEGER NOT NULL,
  finished_at  INTEGER
);

CREATE TABLE IF NOT EXISTS config_templates (
  id         TEXT PRIMARY KEY,
  app_id     TEXT NOT NULL,
  name       TEXT,
  body       TEXT,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS registration_tokens (
  token      TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used       INTEGER NOT NULL DEFAULT 0,
  node_id    TEXT
);

CREATE TABLE IF NOT EXISTS license (
  id           INTEGER PRIMARY KEY CHECK (id = 1),
  key          TEXT NOT NULL DEFAULT '',
  account      TEXT NOT NULL DEFAULT '',
  seats        INTEGER NOT NULL DEFAULT 0,
  expires_at   INTEGER NOT NULL DEFAULT 0,
  activated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS commands (
  id            TEXT PRIMARY KEY,
  node_id       TEXT NOT NULL,
  type          TEXT NOT NULL,
  params        TEXT NOT NULL DEFAULT '{}',
  status        TEXT NOT NULL DEFAULT 'pending',
  created_at    INTEGER NOT NULL,
  finished_at   INTEGER NOT NULL DEFAULT 0,
  result_status TEXT NOT NULL DEFAULT '',
  log           TEXT NOT NULL DEFAULT '',
  data          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_commands_node ON commands(node_id, status);

CREATE TABLE IF NOT EXISTS osupdates (
  node_id    TEXT PRIMARY KEY,
  state      TEXT NOT NULL DEFAULT '{}',
  updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS netdiag (
  node_id    TEXT PRIMARY KEY,
  state      TEXT NOT NULL DEFAULT '{}',
  updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS rollouts (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL DEFAULT '',
  bom_id       TEXT NOT NULL DEFAULT '',
  bom_version  INTEGER NOT NULL DEFAULT 0,
  status       TEXT NOT NULL DEFAULT 'pending',
  current_ring INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  rings        TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS server_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS update_nodes (
  node_id      TEXT PRIMARY KEY,
  target       TEXT NOT NULL DEFAULT '',
  state        TEXT NOT NULL DEFAULT 'pending',
  attempts     INTEGER NOT NULL DEFAULT 0,
  last_attempt INTEGER NOT NULL DEFAULT 0,
  command_id   TEXT NOT NULL DEFAULT '',
  detail       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS mounts (
  id         TEXT PRIMARY KEY,
  host       TEXT NOT NULL,
  share      TEXT NOT NULL,
  target     TEXT NOT NULL,
  username   TEXT NOT NULL DEFAULT '',
  password   TEXT NOT NULL DEFAULT '',
  domain     TEXT NOT NULL DEFAULT '',
  version    TEXT NOT NULL DEFAULT '3.0',
  text_encoding TEXT NOT NULL DEFAULT 'auto',
  writable   INTEGER NOT NULL DEFAULT 0,
  status     TEXT NOT NULL DEFAULT 'unmounted',
  detail     TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  mounted_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tracks (
  id            TEXT PRIMARY KEY,
  mount_id      TEXT NOT NULL REFERENCES mounts(id),
  path          TEXT NOT NULL,
  rel_path      TEXT NOT NULL,
  title         TEXT NOT NULL DEFAULT '',
  artist        TEXT NOT NULL DEFAULT '',
  album         TEXT NOT NULL DEFAULT '',
  codec         TEXT NOT NULL DEFAULT '',
  container     TEXT NOT NULL DEFAULT '',
  rate          INTEGER NOT NULL DEFAULT 0,
  channels      INTEGER NOT NULL DEFAULT 0,
  bits          INTEGER NOT NULL DEFAULT 0,
  total_samples INTEGER NOT NULL DEFAULT 0,
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  decodable     INTEGER NOT NULL DEFAULT 0,
  mqa           INTEGER NOT NULL DEFAULT 0,
  mqa_checked   INTEGER NOT NULL DEFAULT 0,
  size          INTEGER NOT NULL DEFAULT 0,
  scanned_at    INTEGER NOT NULL,
  UNIQUE(mount_id, rel_path)
);
CREATE INDEX IF NOT EXISTS idx_tracks_mount ON tracks(mount_id);

-- Materialized artist index: artist grouping and pinyin keys are computed once
-- per scan instead of on every browse request.
CREATE TABLE IF NOT EXISTS artist_index (
  mode          TEXT NOT NULL,
  name          TEXT NOT NULL,
  sort_key      TEXT NOT NULL,
  initial       TEXT NOT NULL,
  first_char    TEXT NOT NULL,
  track_count   INTEGER NOT NULL DEFAULT 0,
  album_count   INTEGER NOT NULL DEFAULT 0,
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  cover_hash    TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(mode,name)
);
CREATE INDEX IF NOT EXISTS idx_artist_index_initial ON artist_index(mode,initial,sort_key,name);
CREATE INDEX IF NOT EXISTS idx_artist_index_sort ON artist_index(mode,sort_key,name);
CREATE TABLE IF NOT EXISTS artist_index_meta (
  mode      TEXT PRIMARY KEY,
  built_at  INTEGER NOT NULL DEFAULT 0,
  tracks    INTEGER NOT NULL DEFAULT 0
);

-- Identity survives catalog cleanup and temporarily missing files, but not source deletion.
CREATE TABLE IF NOT EXISTS track_identities (
  id       TEXT PRIMARY KEY,
  mount_id TEXT NOT NULL REFERENCES mounts(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL,
  added_at INTEGER NOT NULL CHECK (added_at >= 0),
  UNIQUE(mount_id, rel_path)
);

CREATE TABLE IF NOT EXISTS covers (
  hash       TEXT PRIMARY KEY,
  mime       TEXT NOT NULL,
  data       BLOB NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cover_variants (
  hash       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  mime       TEXT NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  data       BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (hash,size)
);

CREATE TABLE IF NOT EXISTS node_queue (
  node_id       TEXT PRIMARY KEY,
  track_ids     TEXT NOT NULL DEFAULT '[]',
  position      INTEGER NOT NULL DEFAULT 0,
  updated_at    INTEGER NOT NULL DEFAULT 0,
  mode          TEXT NOT NULL DEFAULT 'sequence',
  shuffle_order TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS node_sleep_timer (
  node_id TEXT PRIMARY KEY,
  stop_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS node_devices (
  node_id TEXT NOT NULL,
  hw      TEXT NOT NULL,
  name    TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 0,
  sort    INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, hw)
);

CREATE TABLE IF NOT EXISTS play_history (
  id         TEXT PRIMARY KEY,
  node_id    TEXT NOT NULL,
  track_id   TEXT NOT NULL,
  started_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_history_at ON play_history(started_at);

CREATE TABLE IF NOT EXISTS listen_daily (
  history_id     TEXT NOT NULL REFERENCES play_history(id) ON DELETE CASCADE,
  day            INTEGER NOT NULL,
  listen_ms      INTEGER NOT NULL DEFAULT 0,
  last_played_at INTEGER NOT NULL,
  PRIMARY KEY (history_id, day)
);
CREATE INDEX IF NOT EXISTS idx_listen_day ON listen_daily(day);

CREATE TABLE IF NOT EXISTS playlists (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS playlist_items (
  playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL,
  track_id    TEXT NOT NULL,
  PRIMARY KEY (playlist_id, position)
);

-- Favorites retain metadata when catalog rows disappear; deliberately no tracks FK.
CREATE TABLE IF NOT EXISTS favorites (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL CHECK (kind IN ('track','album')),
  track_id    TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL DEFAULT '',
  artist      TEXT NOT NULL DEFAULT '',
  album       TEXT NOT NULL DEFAULT '',
  cover_hash  TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  codec       TEXT NOT NULL DEFAULT '',
  decodable   INTEGER NOT NULL DEFAULT 0,
  added_at    INTEGER NOT NULL,
  CHECK ((kind='track' AND track_id<>'') OR (kind='album' AND track_id=''))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_favorites_track ON favorites(track_id) WHERE kind='track';
CREATE UNIQUE INDEX IF NOT EXISTS idx_favorites_album ON favorites(artist,album) WHERE kind='album';
CREATE INDEX IF NOT EXISTS idx_favorites_added ON favorites(kind,added_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS apt_sources (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  components TEXT NOT NULL DEFAULT 'main',
  key_url    TEXT NOT NULL DEFAULT '',
  distros    TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL
);
`
