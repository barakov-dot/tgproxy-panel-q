package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// ActorAutoIssue identifies the audit actor for a profile issued
// automatically (auto_issue setting on), as opposed to an admin decision.
const ActorAutoIssue = "auto_issue"

// ActorAdmin builds the audit actor identity for an admin's Telegram-side
// approve/deny decision. The panel's own actions use its own actor string
// (the logged-in admin's login), built by the caller — see
// internal/httpserver.Server.actorName.
func ActorAdmin(adminTelegramID int64) string {
	return "admin:" + strconv.FormatInt(adminTelegramID, 10)
}

// RequestOutcome describes what RequestProxy did in response to a user
// pressing "get proxy".
type RequestOutcome int

const (
	// OutcomeAlreadyActive means the user already had an active profile;
	// nothing was changed, the existing link should be resent.
	OutcomeAlreadyActive RequestOutcome = iota
	// OutcomeIssued means a new profile was generated and applied; the new
	// link should be sent.
	OutcomeIssued
	// OutcomePendingCreated means auto-issue is off and a request was
	// recorded (or an existing revoked/denied row was reopened as
	// pending); the admin should be notified and the user told to wait.
	OutcomePendingCreated
	// OutcomeAlreadyPending means the user already had a request awaiting
	// review; nothing new was created.
	OutcomeAlreadyPending
)

// RequestResult is RequestProxy's outcome plus the resulting user row.
type RequestResult struct {
	Outcome RequestOutcome
	User    *models.User
}

// RequestProxy implements the "user pressed 🔑 Получить прокси" flow
// (plan.md §6): look up any existing request, and either report the
// existing profile, issue a new one (auto-issue on), or record/reopen a
// pending request (auto-issue off).
func (a *Actions) RequestProxy(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*RequestResult, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	switch {
	case err == nil:
		switch u.Status {
		case models.StatusActive:
			return &RequestResult{Outcome: OutcomeAlreadyActive, User: u}, nil
		case models.StatusPending:
			return &RequestResult{Outcome: OutcomeAlreadyPending, User: u}, nil
		}
		// revoked or denied: fall through and treat like a fresh request.
	case errors.Is(err, store.ErrNotFound):
		u, err = a.Store.CreateUser(ctx, telegramID, username, firstName, lastName)
		if err != nil {
			return nil, fmt.Errorf("service: create user telegram_id=%d: %w", telegramID, err)
		}
	default:
		return nil, fmt.Errorf("service: get user telegram_id=%d: %w", telegramID, err)
	}

	autoIssue, err := a.autoIssueEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !autoIssue {
		if u.Status != models.StatusPending {
			// u came from CreateUser above (already pending) or fell
			// through from revoked/denied above (needs reopening) — only
			// the latter needs a write.
			u, err = a.Store.SetPending(ctx, telegramID)
			if err != nil {
				return nil, fmt.Errorf("service: set pending telegram_id=%d: %w", telegramID, err)
			}
		}
		return &RequestResult{Outcome: OutcomePendingCreated, User: u}, nil
	}

	issued, err := a.Approve(ctx, telegramID, ActorAutoIssue)
	if err != nil {
		return nil, err
	}
	return &RequestResult{Outcome: OutcomeIssued, User: issued}, nil
}

func (a *Actions) autoIssueEnabled(ctx context.Context) (bool, error) {
	v, ok, err := a.Store.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil {
		return false, fmt.Errorf("service: get auto_issue setting: %w", err)
	}
	if !ok {
		return a.DefaultAutoIssue, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("service: parse auto_issue setting %q: %w", v, err)
	}
	return b, nil
}
