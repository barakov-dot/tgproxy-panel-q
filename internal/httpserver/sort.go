package httpserver

import (
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

func storeSortColumn(col SortColumn) string {
	switch col {
	case SortTelegramID:
		return "telegram_id"
	case SortName:
		return "username"
	case SortStatus:
		return "status"
	case SortIssuedAt:
		return "issued_at"
	default:
		return "requested_at"
	}
}

func buildListFilter(onlyPending bool, query string) store.UserListFilter {
	filter := store.UserListFilter{Query: query}
	if onlyPending {
		st := models.StatusPending
		filter.Status = &st
	}
	return filter
}

func buildListSort(col SortColumn, desc bool) store.UserListSort {
	return store.UserListSort{
		Column: storeSortColumn(col),
		Desc:   desc,
	}
}
