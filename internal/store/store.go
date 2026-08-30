// Package store provides SQLite-backed persistence for users, settings, and audit_log.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Store wraps a SQLite database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies schema.sql.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("store: apply schema: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

const userColumns = `id, telegram_id, username, first_name, last_name, status, profile_name, secret, requested_at, issued_at, revoked_at`

// UserListFilter optionally restricts ListUsers results.
type UserListFilter struct {
	Status *models.UserStatus
	Query  string
}

// UserListSort controls ListUsers ordering. Column must be one of the allowed sort columns.
type UserListSort struct {
	Column string
	Desc   bool
}

var allowedSortColumns = map[string]string{
	"id":           "id",
	"telegram_id":  "telegram_id",
	"username":     "username",
	"status":       "status",
	"requested_at": "requested_at",
	"issued_at":    "issued_at",
}

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

// CreateUser inserts a new pending user request.
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

// GetUserByID returns a user by primary key.
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

// GetUserByTelegramID returns a user by Telegram ID.
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

// ListUsers returns users matching filter, ordered by sort.
func (s *Store) ListUsers(ctx context.Context, filter UserListFilter, sort UserListSort) ([]*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE 1=1`
	args := []any{}

	if filter.Status != nil {
		query += ` AND status = ?`
		args = append(args, string(*filter.Status))
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		like := "%" + q + "%"
		query += ` AND (
			CAST(telegram_id AS TEXT) LIKE ? OR
			IFNULL(username, '') LIKE ? OR
			IFNULL(first_name, '') LIKE ? OR
			IFNULL(last_name, '') LIKE ?
		)`
		args = append(args, like, like, like, like)
	}

	col := "requested_at"
	if sort.Column != "" {
		if mapped, ok := allowedSortColumns[sort.Column]; ok {
			col = mapped
		}
	}
	dir := "ASC"
	if sort.Desc {
		dir = "DESC"
	}
	query += fmt.Sprintf(` ORDER BY %s %s, id DESC`, col, dir)

	rows, err := s.db.QueryContext(ctx, query, args...)
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

// UpdateUserStatus sets a user's status and related timestamps.
func (s *Store) UpdateUserStatus(ctx context.Context, id int64, status models.UserStatus) (*models.User, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("store: update user status id=%d: invalid status %q", id, status)
	}

	now := formatTime(time.Now())
	var query string
	var args []any

	switch status {
	case models.StatusPending:
		query = `UPDATE users SET status = ?, requested_at = ? WHERE id = ?`
		args = []any{string(status), now, id}
	case models.StatusActive:
		query = `UPDATE users SET status = ? WHERE id = ?`
		args = []any{string(status), id}
	case models.StatusRevoked:
		query = `UPDATE users SET status = ?, revoked_at = ?, profile_name = NULL, secret = NULL WHERE id = ?`
		args = []any{string(status), now, id}
	case models.StatusDenied:
		query = `UPDATE users SET status = ? WHERE id = ?`
		args = []any{string(status), id}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: update user status id=%d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByID(ctx, id)
}

// SetUserProfile sets profile_name and secret for a user. When setActive is true,
// status becomes active and issued_at is set to now.
func (s *Store) SetUserProfile(ctx context.Context, id int64, profileName, secret string, setActive bool) (*models.User, error) {
	var query string
	var args []any
	if setActive {
		query = `UPDATE users SET profile_name = ?, secret = ?, status = ?, issued_at = ? WHERE id = ?`
		args = []any{profileName, secret, string(models.StatusActive), formatTime(time.Now()), id}
	} else {
		query = `UPDATE users SET profile_name = ?, secret = ? WHERE id = ?`
		args = []any{profileName, secret, id}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: set user profile id=%d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByID(ctx, id)
}

// ClearUserProfile removes profile_name and secret without changing status.
func (s *Store) ClearUserProfile(ctx context.Context, id int64) (*models.User, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET profile_name = NULL, secret = NULL WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: clear user profile id=%d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetUserByID(ctx, id)
}

// GetSetting returns a setting value and whether it exists.
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

// AppendAuditLog inserts an audit log row.
func (s *Store) AppendAuditLog(ctx context.Context, entry models.AuditLog) error {
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (created_at, action, actor, user_id, detail)
		VALUES (?, ?, ?, ?, ?)`,
		formatTime(createdAt), entry.Action, entry.Actor, entry.UserID, entry.Detail)
	if err != nil {
		return fmt.Errorf("store: append audit log: %w", err)
	}
	return nil
}

// ListAuditLog returns the newest audit rows up to limit.
func (s *Store) ListAuditLog(ctx context.Context, limit int) ([]models.AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, created_at, action, actor, user_id, detail
		FROM audit_log ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit log: %w", err)
	}
	defer rows.Close()

	var out []models.AuditLog
	for rows.Next() {
		var (
			e          models.AuditLog
			createdAt  string
			userID     sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &createdAt, &e.Action, &e.Actor, &userID, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: list audit log: %w", err)
		}
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("store: list audit log: parse created_at: %w", err)
		}
		if userID.Valid {
			id := userID.Int64
			e.UserID = &id
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list audit log: %w", err)
	}
	return out, nil
}
