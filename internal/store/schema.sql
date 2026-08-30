-- SQLite schema for tgproxy-panel. Applied on first run via store.Migrate().

CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  telegram_id   INTEGER UNIQUE NOT NULL,
  username      TEXT,
  first_name    TEXT,
  last_name     TEXT,
  status        TEXT NOT NULL CHECK(status IN ('pending','active','revoked','denied')),
  profile_name  TEXT UNIQUE,
  secret        TEXT UNIQUE,
  requested_at  DATETIME,
  issued_at     DATETIME,
  revoked_at    DATETIME
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME NOT NULL DEFAULT (datetime('now')),
  action     TEXT NOT NULL,
  actor      TEXT NOT NULL,
  user_id    INTEGER,
  detail     TEXT,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);
