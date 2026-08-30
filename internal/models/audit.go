package models

import "time"

// AuditLog is one audit trail row. Never store profile secrets in Detail.
type AuditLog struct {
	ID        int64
	CreatedAt time.Time
	Action    string
	Actor     string
	UserID    *int64
	Detail    string
}
