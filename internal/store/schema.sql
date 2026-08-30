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
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ts          DATETIME NOT NULL,
  action      TEXT NOT NULL,
  telegram_id INTEGER,
  actor       TEXT NOT NULL,
  detail      TEXT
);
