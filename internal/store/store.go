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
	}
	for _, c := range cols {
		if err := s.addColumnIfMissing(c.table, c.name, c.decl); err != nil {
			return fmt.Errorf("add column %s.%s: %w", c.table, c.name, err)
		}
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

CREATE TABLE IF NOT EXISTS mounts (
  id         TEXT PRIMARY KEY,
  host       TEXT NOT NULL,
  share      TEXT NOT NULL,
  target     TEXT NOT NULL,
  username   TEXT NOT NULL DEFAULT '',
  password   TEXT NOT NULL DEFAULT '',
  domain     TEXT NOT NULL DEFAULT '',
  version    TEXT NOT NULL DEFAULT '3.0',
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
  size          INTEGER NOT NULL DEFAULT 0,
  scanned_at    INTEGER NOT NULL,
  UNIQUE(mount_id, rel_path)
);
CREATE INDEX IF NOT EXISTS idx_tracks_mount ON tracks(mount_id);
`
