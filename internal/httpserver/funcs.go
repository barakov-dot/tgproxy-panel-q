package httpserver

import (
	"fmt"
	"html/template"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

// templateFuncs are helpers exposed to every template — Russian status
// labels/badges, timestamp formatting, and a "dict" helper since
// html/template has no built-in support for any of them, or for passing
// more than one value into a sub-template invocation.
var templateFuncs = template.FuncMap{
	"statusLabel":      statusLabel,
	"statusBadgeClass": statusBadgeClass,
	"formatTime":       formatDisplayTime,
	"formatTimeValue":  formatDisplayTimeValue,
	"deref":            derefString,
	"dict":             dict,
}

// dict builds a map[string]any from alternating key/value arguments, the
// standard html/template trick for passing multiple named values into a
// {{template}} invocation (which otherwise only accepts a single ".").
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
		}
		m[key] = kv[i+1]
	}
	return m, nil
}

func statusLabel(s models.UserStatus) string {
	switch s {
	case models.StatusPending:
		return "Ожидает"
	case models.StatusActive:
		return "Активен"
	case models.StatusRevoked:
		return "Отозван"
	case models.StatusDenied:
		return "Отклонён"
	default:
		return string(s)
	}
}

const badgeBase = "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset"

func statusBadgeClass(s models.UserStatus) string {
	switch s {
	case models.StatusPending:
		return badgeBase + " bg-amber-50 text-amber-800 ring-amber-600/20 dark:bg-amber-400/10 dark:text-amber-400 dark:ring-amber-400/20"
	case models.StatusActive:
		return badgeBase + " bg-emerald-50 text-emerald-800 ring-emerald-600/20 dark:bg-emerald-400/10 dark:text-emerald-400 dark:ring-emerald-400/20"
	case models.StatusRevoked:
		return badgeBase + " bg-slate-100 text-slate-700 ring-slate-500/20 dark:bg-slate-400/10 dark:text-slate-300 dark:ring-slate-400/20"
	case models.StatusDenied:
		return badgeBase + " bg-rose-50 text-rose-700 ring-rose-600/20 dark:bg-rose-400/10 dark:text-rose-400 dark:ring-rose-400/20"
	default:
		return badgeBase
	}
}

func formatDisplayTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return formatDisplayTimeValue(*t)
}

func formatDisplayTimeValue(t time.Time) string {
	return t.Local().Format("02.01.2006 15:04")
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
