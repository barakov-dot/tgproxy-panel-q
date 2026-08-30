package service

import (
	"context"
	"errors"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

func TestDelete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		status    models.UserStatus
		wantErr   error
		wantAudit bool
	}{
		{
			name:      "revoked user",
			status:    models.StatusRevoked,
			wantAudit: true,
		},
		{
			name:      "denied user",
			status:    models.StatusDenied,
			wantAudit: true,
		},
		{
			name:    "active user rejected",
			status:  models.StatusActive,
			wantErr: ErrNotDeletable,
		},
		{
			name:    "pending user rejected",
			status:  models.StatusPending,
			wantErr: ErrNotDeletable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeStore()
			fa := &fakeApplier{}
			svc := testService(fs, fa, nil, false)

			u := fs.addUser(&models.User{TelegramID: 1001, Status: tt.status})

			err := svc.Delete(ctx, u.ID, "admin")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Delete() err = %v, want %v", err, tt.wantErr)
				}
				if _, getErr := fs.GetUserByID(ctx, u.ID); getErr != nil {
					t.Fatal("user should still exist after failed delete")
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() err = %v", err)
			}
			if _, getErr := fs.GetUserByID(ctx, u.ID); !errors.Is(getErr, store.ErrNotFound) {
				t.Fatalf("user should be gone, GetUserByID err = %v", getErr)
			}
			if tt.wantAudit && !hasAuditAction(fs, auditActionDelete) {
				t.Error("expected delete audit entry")
			}
			if fa.IssueCalls != 0 || fa.RevokeCalls != 0 {
				t.Error("delete should not touch applier")
			}
		})
	}
}
