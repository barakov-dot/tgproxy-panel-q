package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/service"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

type usersListPageData struct {
	pageData
	usersTableData
}

type usersTableData struct {
	Users        []*models.User
	Query        string
	Sort         SortColumn
	Dir          string
	OnlyPending  bool
	PendingCount int
}

func (s *Server) userListData(r *http.Request) (usersTableData, error) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		return usersTableData{}, err
	}
	pendingCount := len(FilterPending(users))

	q := r.URL.Query().Get("q")
	col := parseSortColumn(r.URL.Query().Get("sort"))
	dirParam := r.URL.Query().Get("dir")
	desc := parseSortDir(dirParam)
	dir := "desc"
	if !desc {
		dir = "asc"
	}
	onlyPending := r.URL.Query().Get("filter") == "pending"

	filtered := FilterAndSort(users, q, col, desc)
	if onlyPending {
		filtered = FilterPending(filtered)
	}

	return usersTableData{
		Users:        filtered,
		Query:        q,
		Sort:         col,
		Dir:          dir,
		OnlyPending:  onlyPending,
		PendingCount: pendingCount,
	}, nil
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	data, err := s.userListData(r)
	if err != nil {
		s.log.Error("list users", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "users_list.html", usersListPageData{
		pageData:       s.newPageData("users"),
		usersTableData: data,
	})
}

// handleUserTable serves the htmx partial (just the table body + header
// links) used by both live search (hx-trigger="keyup changed delay:300ms")
// and click-to-sort column headers, so neither triggers a full page reload.
func (s *Server) handleUserTable(w http.ResponseWriter, r *http.Request) {
	data, err := s.userListData(r)
	if err != nil {
		s.log.Error("list users (table partial)", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "users_table.html", struct {
		pageData
		usersTableData
	}{pageData: s.newPageData("users"), usersTableData: data})
}

type userDetailPageData struct {
	pageData
	User    *models.User
	Link    string
	Error   string
	Message string
}

// profileLink builds the t.me deep link per plan.md §5:
// https://t.me/webproxy?server=<host>&secret=<secret>
func profileLink(hostname, secret string) string {
	if secret == "" {
		return ""
	}
	v := "https://t.me/webproxy?server=" + hostname + "&secret=" + secret
	return v
}

func (s *Server) userByIDParam(r *http.Request) (*models.User, error) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, store.ErrNotFound
	}
	return s.store.GetUserByID(r.Context(), id)
}

func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	s.render(w, r, "user_detail.html", s.userDetailData(u, "", ""))
}

func (s *Server) userDetailData(u *models.User, message, errMsg string) userDetailPageData {
	link := ""
	if u.Secret != nil {
		link = profileLink(s.cfg.TproxyHostname, *u.Secret)
	}
	return userDetailPageData{
		pageData: s.newPageData("users"),
		User:     u,
		Link:     link,
		Message:  message,
		Error:    errMsg,
	}
}

// renderUserDetail renders just the swappable detail section (used by the
// approve/deny/revoke handlers, whose htmx requests target
// #user-detail-content and expect only that fragment back — swapping a
// full <html> document into it would nest a document inside a div).
func (s *Server) renderUserDetail(w http.ResponseWriter, r *http.Request, u *models.User, message, errMsg string) {
	s.render(w, r, "user_detail_content.html", s.userDetailData(u, message, errMsg))
}

func (s *Server) notFoundOrError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.log.Error("lookup user", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// actorName identifies who performed a state-changing action, for the audit
// log. There is exactly one admin account (plan.md §5), so the configured
// login is a stable, sufficient actor identity.
func (s *Server) actorName() string {
	return "panel:" + s.cfg.AdminLogin
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.runAction(w, r, func(ctx context.Context, telegramID int64) (*models.User, error) {
		return s.actions.Approve(ctx, telegramID, s.actorName())
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	s.runAction(w, r, func(ctx context.Context, telegramID int64) (*models.User, error) {
		return s.actions.Revoke(ctx, telegramID, s.actorName())
	})
}

func (s *Server) handleDeny(w http.ResponseWriter, r *http.Request) {
	s.runAction(w, r, func(ctx context.Context, telegramID int64) (*models.User, error) {
		return s.actions.Deny(ctx, telegramID, s.actorName())
	})
}

// handleNotify is a placeholder for plan.md §5's "отправить пользователю в
// Telegram" button. Actually sending the message needs a live bot client
// (internal/bot), which cmd/tgproxy-panel/main.go wires up alongside this
// server — until main.go passes a sender through, this just tells the admin
// so, rather than silently doing nothing or 404ing.
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	s.renderUserDetail(w, r, u, "", "Отправка через бота пока не подключена. Скопируйте ссылку вручную.")
}

// runAction loads the user targeted by the {id} path param, runs action
// against its telegram_id, and re-renders the user_detail partial with
// either the fresh state or a Russian, user-facing error — this is what
// makes the buttons on the detail page htmx-friendly (hx-post + hx-target
// swapping the detail section) rather than a full-page redirect.
func (s *Server) runAction(w http.ResponseWriter, r *http.Request, action func(context.Context, int64) (*models.User, error)) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}

	updated, err := action(r.Context(), u.TelegramID)
	if err != nil {
		msg := userFacingActionError(err)
		// updated is nil on hard failure; re-fetch current state so the
		// page still reflects reality instead of going blank.
		current, lookupErr := s.store.GetUserByTelegramID(r.Context(), u.TelegramID)
		if lookupErr != nil {
			s.log.Error("re-lookup user after failed action", "error", lookupErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.log.Error("user action failed", "action_error", err, "telegram_id", u.TelegramID)
		s.renderUserDetail(w, r, current, "", msg)
		return
	}
	s.renderUserDetail(w, r, updated, "Готово.", "")
}

// userFacingActionError translates internal/service's sentinel errors into
// the Russian copy shown on the detail page. Everything else (a real DB or
// applier error) gets one generic message — the exact cause is logged
// server-side, not shown to the browser.
func userFacingActionError(err error) string {
	switch {
	case errors.Is(err, service.ErrAlreadyActive):
		return "У пользователя уже есть активный профиль."
	case errors.Is(err, service.ErrNotPending):
		return "Заявка больше не ожидает рассмотрения."
	case errors.Is(err, service.ErrNotActive):
		return "У пользователя нет активного профиля для отзыва."
	case errors.Is(err, service.ErrIssueFailed):
		return "Не удалось выдать профиль на сервере. Изменения отменены, повторите попытку или обратитесь к администратору."
	case errors.Is(err, service.ErrRevokeApplyFailed):
		return "Статус изменён на «отозван», но применить изменения на сервере не удалось. Повторите попытку."
	default:
		return "Внутренняя ошибка. Попробуйте ещё раз."
	}
}
