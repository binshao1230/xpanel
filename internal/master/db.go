package master

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// resolveDBPath prefers bpanel.db; migrates legacy xpanel.db if present.
func resolveDBPath(dataDir string) string {
	dir := filepath.Clean(dataDir)
	primary := filepath.Join(dir, "bpanel.db")
	legacy := filepath.Join(dir, "xpanel.db")
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	if _, err := os.Stat(legacy); err == nil {
		// best-effort rename so old installs keep data under the new brand
		if err := os.Rename(legacy, primary); err == nil {
			// also rename WAL/SHM sidecars if present
			_ = os.Rename(legacy+"-wal", primary+"-wal")
			_ = os.Rename(legacy+"-shm", primary+"-shm")
			return primary
		}
		return legacy
	}
	return primary
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  subscribe_token TEXT NOT NULL UNIQUE,
  plan_id INTEGER NOT NULL DEFAULT 0,
  traffic_limit INTEGER NOT NULL DEFAULT 0,
  traffic_used INTEGER NOT NULL DEFAULT 0,
  speed_limit INTEGER NOT NULL DEFAULT 0,
  expire_at INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  remark TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  traffic_limit INTEGER NOT NULL DEFAULT 0,
  speed_limit INTEGER NOT NULL DEFAULT 0,
  duration_days INTEGER NOT NULL DEFAULT 30,
  price_note TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  install_token TEXT NOT NULL UNIQUE,
  agent_key TEXT NOT NULL DEFAULT '',
  hostname TEXT NOT NULL DEFAULT '',
  public_ip TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  last_seen INTEGER NOT NULL DEFAULT 0,
  config_version INTEGER NOT NULL DEFAULT 0,
  agent_version TEXT NOT NULL DEFAULT '',
  xray_running INTEGER NOT NULL DEFAULT 0,
  traffic_up INTEGER NOT NULL DEFAULT 0,
  traffic_down INTEGER NOT NULL DEFAULT 0,
  conn_mode TEXT NOT NULL DEFAULT 'http',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS inbounds (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  protocol TEXT NOT NULL,
  port INTEGER NOT NULL,
  settings_json TEXT NOT NULL DEFAULT '{}',
  stream_json TEXT NOT NULL DEFAULT '{}',
  multiplier REAL NOT NULL DEFAULT 1,
  remark TEXT NOT NULL DEFAULT '',
  cert_id INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outbounds (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  protocol TEXT NOT NULL,
  settings_json TEXT NOT NULL DEFAULT '{}',
  stream_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS route_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  outbound_tag TEXT NOT NULL,
  domain_json TEXT NOT NULL DEFAULT '[]',
  ip_json TEXT NOT NULL DEFAULT '[]',
  port TEXT NOT NULL DEFAULT '',
  network TEXT NOT NULL DEFAULT '',
  protocol_json TEXT NOT NULL DEFAULT '[]',
  priority INTEGER NOT NULL DEFAULT 100,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS external_nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,
  share_link TEXT NOT NULL DEFAULT '',
  raw_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS certificates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  domain TEXT NOT NULL DEFAULT '',
  cert_pem TEXT NOT NULL DEFAULT '',
  key_pem TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT 'manual',
  expire_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  challenge TEXT NOT NULL DEFAULT '',
  dns_provider TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  last_error TEXT NOT NULL DEFAULT '',
  auto_renew INTEGER NOT NULL DEFAULT 1,
  server_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS traffic_daily (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  day TEXT NOT NULL,
  user_id INTEGER NOT NULL DEFAULT 0,
  server_id TEXT NOT NULL DEFAULT '',
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  UNIQUE(day, user_id, server_id)
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_node_access (
  user_id INTEGER NOT NULL,
  inbound_id INTEGER NOT NULL,
  PRIMARY KEY(user_id, inbound_id)
);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	alters := []string{
		`ALTER TABLE servers ADD COLUMN xray_running INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE servers ADD COLUMN traffic_up INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE servers ADD COLUMN traffic_down INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE servers ADD COLUMN conn_mode TEXT NOT NULL DEFAULT 'http'`,
		`ALTER TABLE users ADD COLUMN plan_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN speed_limit INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE users ADD COLUMN remark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE inbounds ADD COLUMN multiplier REAL NOT NULL DEFAULT 1`,
		`ALTER TABLE inbounds ADD COLUMN remark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certificates ADD COLUMN email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certificates ADD COLUMN challenge TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certificates ADD COLUMN dns_provider TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certificates ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE certificates ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE certificates ADD COLUMN auto_renew INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE certificates ADD COLUMN server_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE inbounds ADD COLUMN cert_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE servers ADD COLUMN agent_error TEXT NOT NULL DEFAULT ''`,
	}
	for _, q := range alters {
		_, _ = db.Exec(q)
	}
	for _, q := range migrateV5SQL() {
		_, _ = db.Exec(q)
	}
	// heal broken configs from earlier releases
	_, _ = db.Exec(`UPDATE outbounds SET enabled=0 WHERE protocol='wireguard' AND settings_json LIKE '%REPLACE%'`)
	_, _ = db.Exec(`UPDATE route_rules SET enabled=0 WHERE domain_json LIKE '%geosite:%' OR domain_json LIKE '%geoip:%'`)
	return nil
}

func nowUnix() int64 { return time.Now().Unix() }

func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
