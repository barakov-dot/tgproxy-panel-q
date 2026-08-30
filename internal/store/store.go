// Package store provides SQLite-backed access to the users, settings and
// audit_log tables (schema.sql, matching plan.md §4). There is no migration
// framework — the schema is small and stable; startup just runs
// CREATE TABLE IF NOT EXISTS.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned by lookups and updates that target a row which
// does not exist.
var ErrNotFound = errors.New("store: not found")

// Store wraps a SQLite database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	// modernc.org/sqlite does not support concurrent writers on one file; a
	// single pooled connection serializes all access instead of racing
	// multiple connections into SQLITE_BUSY. Fine at this project's ~50-user
	// scale (plan.md §13).
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

const userColumns = `id, telegram_id, username, first_name, last_name, status, profile_name, secret, requested_at, issued_at, revoked_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*models.User, error) {
	var (
		u                                                  models.User
		status                                             string
		username, firstName, lastName, profileName, secret sql.NullString
		requestedAt, issuedAt, revokedAt                   sql.NullString
	)
	if err := row.Scan(&u.ID, &u.TelegramID, &username, &firstName, &lastName, &status,
		&profileName, &secret, &requestedAt, &issuedAt, &revokedAt); err != nil {
		return nil, err
	}

	u.Status = models.UserStatus(status)
	u.Username = nullStringToPtr(username)
	u.FirstName = nullStringToPtr(firstName)
	u.LastName = nullStringToPtr(lastName)
	u.ProfileName = nullStringToPtr(profileName)
	u.Secret = nullStringToPtr(secret)

	var err error
	if u.RequestedAt, err = nullStringToTime(requestedAt); err != nil {
		return nil, fmt.Errorf("requested_at: %w", err)
	}
	if u.IssuedAt, err = nullStringToTime(issuedAt); err != nil {
		return nil, fmt.Errorf("issued_at: %w", err)
	}
	if u.RevokedAt, err = nullStringToTime(revokedAt); err != nil {
		return nil, fmt.Errorf("revoked_at: %w", err)
	}
	return &u, nil
}

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullStringToTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// CreateUser inserts a new pending request for telegramID. username,
// firstName and lastName may be nil. requested_at is set to now.
func (s *Store) CreateUser(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (telegram_id, username, first_name, last_name, status, requested_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		telegramID, username, firstName, lastName, string(models.StatusPending), formatTime(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("store: create user telegram_id=%d: %w", telegramID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: create user telegram_id=%d: %w", telegramID, err)
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByID returns the user with the given primary key, or ErrNotFound.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user id=%d: %w", id, err)
	}
	return u, nil
}

// GetUserByTelegramID returns the user with the given Telegram ID, or
// ErrNotFound.
func (s *Store) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE telegram_id = ?`, telegramID)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user telegram_id=%d: %w", telegramID, err)
	}
	return u, nil
}

// ListUsers returns every user, most recently requested first. Sorting by
// other columns and searching are left to the caller (httpserver, over this
// slice or with its own query) per CLAUDE.md's stage-2 scope.
func (s *Store) ListUsers(ctx context.Context) ([]*models.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY requested_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	var out []*models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list users: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	return out, nil
}

// IssueUser grants telegramID an active profile: sets profile_name, secret,
// status=active and issued_at=now. The user must already exist (created via
// CreateUser when the request first came in); returns ErrNotFound otherwise.
func (s *Store) IssueUser(ctx context.Context, telegramID int64, profileName, secret string) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET profile_name = ?, secret = ?, status = ?, issued_at = ?
		WHERE telegram_id = ?`,
		profileName, secret, string(models.StatusActive), formatTime(time.Now()), telegramID)
	if err != nil {
		return nil, fmt.Errorf("store: issue user telegram_id=%d: %w", telegramID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByTelegramID(ctx, telegramID)
}

// RevokeUser marks telegramID's profile revoked: status=revoked,
// revoked_at=now, and clears profile_name/secret.
//
// Design note: users.telegram_id is UNIQUE, so a given Telegram user always
// maps to exactly one row that IssueUser updates in place — a future
// re-issue for the same telegram_id never needs profile_name/secret freed
// up to avoid a UNIQUE conflict on *this* row. We still null them on revoke
// because (1) the secret is dead the moment it's removed from
// profiles.json, so there's no reason to keep it sitting in the DB in
// plaintext, and (2) it frees those UNIQUE slots so a brand-new, unrelated
// user's randomly generated secret can never collide with a long-revoked
// one. requested_at/issued_at/revoked_at plus the audit_log rows already
// preserve the history that this user was issued and later revoked.
func (s *Store) RevokeUser(ctx context.Context, telegramID int64) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET profile_name = NULL, secret = NULL, status = ?, revoked_at = ?
		WHERE telegram_id = ?`,
		string(models.StatusRevoked), formatTime(time.Now()), telegramID)
	if err != nil {
		return nil, fmt.Errorf("store: revoke user telegram_id=%d: %w", telegramID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByTelegramID(ctx, telegramID)
}

// DenyUser marks a pending request denied, without touching any other
// field.
func (s *Store) DenyUser(ctx context.Context, telegramID int64) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ? WHERE telegram_id = ?`,
		string(models.StatusDenied), telegramID)
	if err != nil {
		return nil, fmt.Errorf("store: deny user telegram_id=%d: %w", telegramID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByTelegramID(ctx, telegramID)
}

// SetPending resets telegramID's row to a fresh pending request:
// status=pending, requested_at=now. Used when a previously revoked or
// denied user re-requests access with auto-issue off, so the request
// reappears in the panel's pending queue and an admin decision is expected
// again, instead of silently staying revoked/denied while a notification is
// sent (see internal/service.RequestProxy).
func (s *Store) SetPending(ctx context.Context, telegramID int64) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET status = ?, requested_at = ? WHERE telegram_id = ?`,
		string(models.StatusPending), formatTime(time.Now()), telegramID)
	if err != nil {
		return nil, fmt.Errorf("store: set pending telegram_id=%d: %w", telegramID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByTelegramID(ctx, telegramID)
}

// GetSetting returns a setting's value and whether it was found.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get setting %s: %w", key, err)
	}
	return value, true, nil
}

// SetSetting creates or updates a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("store: set setting %s: %w", key, err)
	}
	return nil
}

// WriteAuditLog records one audit trail entry (plan.md §7 step 7). If
// entry.Timestamp is zero it defaults to now. entry.Detail must never
// contain a profile secret (see internal/logging's convention).
func (s *Store) WriteAuditLog(ctx context.Context, entry models.AuditLog) error {
	ts := entry.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (ts, action, telegram_id, actor, detail)
		VALUES (?, ?, ?, ?, ?)`,
		formatTime(ts), entry.Action, entry.TelegramID, entry.Actor, entry.Detail)
	if err != nil {
		return fmt.Errorf("store: write audit log: %w", err)
	}
	return nil
}

// ListAuditLog returns the most recent audit rows, newest first, up to
// limit.
func (s *Store) ListAuditLog(ctx context.Context, limit int) ([]models.AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ts, action, telegram_id, actor, detail
		FROM audit_log ORDER BY ts DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit log: %w", err)
	}
	defer rows.Close()

	var out []models.AuditLog
	for rows.Next() {
		var (
			e  models.AuditLog
			ts string
		)
		if err := rows.Scan(&e.ID, &ts, &e.Action, &e.TelegramID, &e.Actor, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: list audit log: %w", err)
		}
		e.Timestamp, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("store: list audit log: parse ts: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list audit log: %w", err)
	}
	return out, nil
}
