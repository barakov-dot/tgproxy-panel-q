package models

import "time"

// AuditLog is one row of the audit trail written for every state-changing
// operation (plan.md §7 step 7). It intentionally never carries a profile
// secret — only enough to reconstruct who did what to whom, and when.
type AuditLog struct {
	ID         int64
	Timestamp  time.Time
	Action     string
	TelegramID *int64
	Actor      string
	Detail     string
}
