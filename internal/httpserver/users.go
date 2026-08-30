package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
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
	q := r.URL.Query().Get("q")
	col := parseSortColumn(r.URL.Query().Get("sort"))
	desc := parseSortDir(r.URL.Query().Get("dir"))
	dir := "desc"
	if !desc {
		dir = "asc"
	}
	onlyPending := r.URL.Query().Get("filter") == "pending"

	users, err := s.svc.ListUsers(r.Context(), buildListFilter(onlyPending, q), buildListSort(col, desc))
	if err != nil {
		return usersTableData{}, err
	}
	if col == SortName {
		sortUsers(users, SortName, desc)
	}

	pendingCount, err := s.svc.CountPendingUsers(r.Context())
	if err != nil {
		return usersTableData{}, err
	}

	return usersTableData{
		Users:        users,
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

func (s *Server) userByIDParam(r *http.Request) (*models.User, error) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, store.ErrNotFound
	}
	return s.svc.GetUser(r.Context(), id)
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
	return userDetailPageData{
		pageData: s.newPageData("users"),
		User:     u,
		Link:     s.svc.GetProxyLink(u),
		Message:  message,
		Error:    errMsg,
	}
}

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

func (s *Server) actorName() string {
	return "panel:" + s.cfg.AdminLogin
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.runUserAction(w, r, func(ctx context.Context, userID int64) (*models.User, error) {
		return s.svc.Approve(ctx, userID, s.actorName())
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	s.runUserAction(w, r, func(ctx context.Context, userID int64) (*models.User, error) {
		return s.svc.Revoke(ctx, userID, s.actorName())
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}

	if err := s.svc.Delete(r.Context(), u.ID, s.actorName()); err != nil {
		msg := userFacingActionError(err)
		current, lookupErr := s.svc.GetUser(r.Context(), u.ID)
		if lookupErr != nil {
			s.log.Error("re-lookup user after failed delete", "error", lookupErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.log.Error("delete user failed", "error", err, "user_id", u.ID)
		s.renderUserDetail(w, r, current, "", msg)
		return
	}

	w.Header().Set("HX-Redirect", s.base()+"/")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeny(w http.ResponseWriter, r *http.Request) {
	s.runUserAction(w, r, func(ctx context.Context, userID int64) (*models.User, error) {
		return s.svc.Deny(ctx, userID, s.actorName())
	})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	if err := s.svc.Resend(r.Context(), u.ID, s.actorName()); err != nil {
		msg := userFacingSendError(err)
		current, lookupErr := s.svc.GetUser(r.Context(), u.ID)
		if lookupErr != nil {
			s.log.Error("re-lookup user after failed send", "error", lookupErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.log.Error("send proxy link failed", "error", err, "user_id", u.ID)
		s.renderUserDetail(w, r, current, "", msg)
		return
	}
	current, err := s.svc.GetUser(r.Context(), u.ID)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	s.renderUserDetail(w, r, current, "Ссылка отправлена пользователю в Telegram.", "")
}

func (s *Server) runUserAction(w http.ResponseWriter, r *http.Request, action func(context.Context, int64) (*models.User, error)) {
	u, err := s.userByIDParam(r)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}

	updated, err := action(r.Context(), u.ID)
	if err != nil {
		msg := userFacingActionError(err)
		current, lookupErr := s.svc.GetUser(r.Context(), u.ID)
		if lookupErr != nil {
			s.log.Error("re-lookup user after failed action", "error", lookupErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.log.Error("user action failed", "action_error", err, "user_id", u.ID)
		s.renderUserDetail(w, r, current, "", msg)
		return
	}
	s.renderUserDetail(w, r, updated, "Готово.", "")
}

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
	case errors.Is(err, service.ErrNotDeletable):
		return "Удалить можно только пользователей со статусом «отозван» или «отклонён»."
	default:
		return "Внутренняя ошибка. Попробуйте ещё раз."
	}
}

func userFacingSendError(err error) string {
	switch {
	case errors.Is(err, service.ErrNotActive):
		return "У пользователя нет активного профиля."
	case errors.Is(err, service.ErrNoProxyLink):
		return "Не удалось построить ссылку на прокси."
	case errors.Is(err, service.ErrNoBotSender):
		return "Отправка через бота пока не подключена. Скопируйте ссылку вручную."
	default:
		return "Не удалось отправить сообщение. Попробуйте ещё раз."
	}
}
