package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

// RequestOutcome describes what Request did in response to a proxy request.
type RequestOutcome int

const (
	OutcomeAlreadyActive RequestOutcome = iota
	OutcomeIssued
	OutcomePendingCreated
	OutcomeAlreadyPending
)

// RequestResult is Request's outcome plus the resulting user row.
type RequestResult struct {
	Outcome RequestOutcome
	User    *models.User
}

// ActorAdmin builds the audit actor identity for a Telegram admin action.
func ActorAdmin(adminTelegramID int64) string {
	return "admin:" + strconv.FormatInt(adminTelegramID, 10)
}

// Request implements the bot "get proxy" flow: return existing credentials,
// auto-issue, or create/reopen a pending request.
func (s *Service) Request(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*RequestResult, error) {
	u, err := s.store.GetUserByTelegramID(ctx, telegramID)
	switch {
	case err == nil:
		switch u.Status {
		case models.StatusActive:
			return &RequestResult{Outcome: OutcomeAlreadyActive, User: u}, nil
		case models.StatusPending:
			return &RequestResult{Outcome: OutcomeAlreadyPending, User: u}, nil
		}
	case errors.Is(err, store.ErrNotFound):
		u, err = s.store.CreateUser(ctx, telegramID, username, firstName, lastName)
		if err != nil {
			return nil, fmt.Errorf("service: create user telegram_id=%d: %w", telegramID, err)
		}
	default:
		return nil, fmt.Errorf("service: get user telegram_id=%d: %w", telegramID, err)
	}

	autoIssue, err := s.AutoIssueEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: auto_issue check telegram_id=%d: %w", telegramID, err)
	}
	if !autoIssue {
		if u.Status != models.StatusPending {
			u, err = s.store.UpdateUserStatus(ctx, u.ID, models.StatusPending)
			if err != nil {
				return nil, fmt.Errorf("service: set pending user id=%d: %w", u.ID, err)
			}
		}
		return &RequestResult{Outcome: OutcomePendingCreated, User: u}, nil
	}

	issued, err := s.Issue(ctx, u, ActorAutoIssue)
	if err != nil {
		return nil, err
	}
	return &RequestResult{Outcome: OutcomeIssued, User: issued}, nil
}
