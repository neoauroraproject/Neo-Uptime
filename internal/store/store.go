// Package store is the SQLite persistence layer.
//
// Two pools are opened on the same WAL database: a single-connection writer,
// so writes never race each other into SQLITE_BUSY, and a small reader pool so
// a slow chart query never holds up a heartbeat being written.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// DB wraps the reader and writer pools.
type DB struct {
	w *sql.DB
	r *sql.DB
}

// Open opens (and migrates) the database at path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?" + url.Values{
		"_pragma": {
			"busy_timeout(10000)",
			"journal_mode(WAL)",
			"foreign_keys(1)",
			"synchronous(NORMAL)",
		},
		"_txlock": {"immediate"},
	}.Encode()

	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)

	db := &DB{w: w, r: r}
	if err := db.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close closes both pools.
func (db *DB) Close() error {
	err1 := db.r.Close()
	err2 := db.w.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func now() int64   { return time.Now().Unix() }
func nowMs() int64 { return time.Now().UnixMilli() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// tx runs fn inside a write transaction.
func (db *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	t, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

var migrations = []string{
	// v1 — initial schema.
	`
CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_login INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE TABLE api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  scope TEXT NOT NULL DEFAULT 'read',
  user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL DEFAULT 0,
  last_used INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE monitors (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  parent_id INTEGER REFERENCES monitors(id) ON DELETE SET NULL,
  active INTEGER NOT NULL DEFAULT 1,
  interval INTEGER NOT NULL DEFAULT 60,
  retry_interval INTEGER NOT NULL DEFAULT 60,
  max_retries INTEGER NOT NULL DEFAULT 1,
  timeout INTEGER NOT NULL DEFAULT 30,
  resend_interval INTEGER NOT NULL DEFAULT 0,
  upside_down INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT '',
  config TEXT NOT NULL DEFAULT '{}',
  push_token TEXT,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_monitors_push ON monitors(push_token) WHERE push_token IS NOT NULL;
CREATE TABLE heartbeats (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status INTEGER NOT NULL,
  time INTEGER NOT NULL,
  ping REAL,
  msg TEXT NOT NULL DEFAULT '',
  important INTEGER NOT NULL DEFAULT 0,
  duration INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER,
  bytes_out INTEGER
);
CREATE INDEX idx_hb_monitor_time ON heartbeats(monitor_id, time);
CREATE INDEX idx_hb_important ON heartbeats(time) WHERE important = 1;
CREATE TABLE stat_hourly (
  monitor_id INTEGER NOT NULL,
  ts INTEGER NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  ping_sum REAL NOT NULL DEFAULT 0,
  ping_cnt INTEGER NOT NULL DEFAULT 0,
  ping_min REAL,
  ping_max REAL,
  PRIMARY KEY (monitor_id, ts)
) WITHOUT ROWID;
CREATE TABLE stat_daily (
  monitor_id INTEGER NOT NULL,
  ts INTEGER NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  ping_sum REAL NOT NULL DEFAULT 0,
  ping_cnt INTEGER NOT NULL DEFAULT 0,
  ping_min REAL,
  ping_max REAL,
  PRIMARY KEY (monitor_id, ts)
) WITHOUT ROWID;
CREATE TABLE tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  color TEXT NOT NULL DEFAULT '#22c55e',
  created_at INTEGER NOT NULL
);
CREATE TABLE monitor_tags (
  monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  value TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (monitor_id, tag_id, value)
);
CREATE TABLE notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  is_default INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);
CREATE TABLE monitor_notifications (
  monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
  PRIMARY KEY (monitor_id, notification_id)
);
CREATE TABLE status_pages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  theme TEXT NOT NULL DEFAULT 'aurora',
  layout TEXT NOT NULL DEFAULT 'list',
  bar_mode TEXT NOT NULL DEFAULT 'heartbeat',
  accent_color TEXT NOT NULL DEFAULT '',
  logo TEXT NOT NULL DEFAULT '',
  footer TEXT NOT NULL DEFAULT '',
  custom_css TEXT NOT NULL DEFAULT '',
  show_tags INTEGER NOT NULL DEFAULT 0,
  show_cert_expiry INTEGER NOT NULL DEFAULT 0,
  show_powered_by INTEGER NOT NULL DEFAULT 1,
  show_charts INTEGER NOT NULL DEFAULT 1,
  published INTEGER NOT NULL DEFAULT 1,
  refresh_interval INTEGER NOT NULL DEFAULT 60,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE status_page_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status_page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE status_page_monitors (
  group_id INTEGER NOT NULL REFERENCES status_page_groups(id) ON DELETE CASCADE,
  monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  show_url INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (group_id, monitor_id)
);
CREATE TABLE incidents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status_page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  style TEXT NOT NULL DEFAULT 'warning',
  pinned INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE maintenances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  strategy TEXT NOT NULL DEFAULT 'manual',
  active INTEGER NOT NULL DEFAULT 1,
  start_at INTEGER NOT NULL DEFAULT 0,
  end_at INTEGER NOT NULL DEFAULT 0,
  weekdays TEXT NOT NULL DEFAULT '[]',
  days_of_month TEXT NOT NULL DEFAULT '[]',
  time_from TEXT NOT NULL DEFAULT '00:00',
  time_to TEXT NOT NULL DEFAULT '00:00',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE maintenance_monitors (
  maintenance_id INTEGER NOT NULL REFERENCES maintenances(id) ON DELETE CASCADE,
  monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  PRIMARY KEY (maintenance_id, monitor_id)
);
CREATE TABLE maintenance_status_pages (
  maintenance_id INTEGER NOT NULL REFERENCES maintenances(id) ON DELETE CASCADE,
  status_page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  PRIMARY KEY (maintenance_id, status_page_id)
);
CREATE TABLE backpack_panels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  token TEXT NOT NULL DEFAULT '',
  ignore_tls INTEGER NOT NULL DEFAULT 1,
  interval INTEGER NOT NULL DEFAULT 30,
  auto_import INTEGER NOT NULL DEFAULT 1,
  group_monitor_id INTEGER REFERENCES monitors(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
`,
	// v2 — agents, owner, flags, private status API, page language.
	`
ALTER TABLE monitors ADD COLUMN country TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN is_owner INTEGER NOT NULL DEFAULT 0;
UPDATE users SET is_owner = 1 WHERE id = (SELECT MIN(id) FROM users WHERE role = 'admin');
ALTER TABLE status_pages ADD COLUMN language TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE status_pages ADD COLUMN show_uptime INTEGER NOT NULL DEFAULT 1;
ALTER TABLE status_pages ADD COLUMN show_latency INTEGER NOT NULL DEFAULT 1;
ALTER TABLE status_pages ADD COLUMN show_banner INTEGER NOT NULL DEFAULT 1;
CREATE TABLE agents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  token_prefix TEXT NOT NULL,
  country TEXT NOT NULL DEFAULT '',
  interval INTEGER NOT NULL DEFAULT 20,
  auto_import INTEGER NOT NULL DEFAULT 1,
  group_monitor_id INTEGER REFERENCES monitors(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  hostname TEXT NOT NULL DEFAULT ''
);
CREATE TABLE status_api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  status_page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  allowed_origins TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1
);
`,
	// v3 — a domain per status page, uploaded images (logos), day-bar range.
	`
ALTER TABLE status_pages ADD COLUMN domain TEXT NOT NULL DEFAULT '';
ALTER TABLE status_pages ADD COLUMN bar_days INTEGER NOT NULL DEFAULT 90;
CREATE UNIQUE INDEX status_pages_domain ON status_pages(domain) WHERE domain <> '';
CREATE TABLE uploads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  mime TEXT NOT NULL,
  data BLOB NOT NULL,
  created_at INTEGER NOT NULL
);
`,
}

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.w.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	err := db.w.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.w.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
		v = 0
	} else if err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		err := db.tx(ctx, func(t *sql.Tx) error {
			if _, err := t.ExecContext(ctx, migrations[i]); err != nil {
				return err
			}
			_, err := t.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}
