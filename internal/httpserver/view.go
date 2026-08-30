package httpserver

import (
	"net/http"
)

// pageData is embedded in every full-page template's data so base.html's
// layout (nav, active-page highlighting) has what it needs. Handlers embed
// this by value and add their own fields alongside it.
type pageData struct {
	Base       string
	ActivePage string
	Flash      string
	FlashKind  string // "error" or "" (success/neutral)
}

func (s *Server) newPageData(active string) pageData {
	return pageData{Base: s.base(), ActivePage: active}
}

// render executes the named template into w, writing a 500 page if
// execution fails partway (best-effort — html/template already streams
// output, so a mid-render error can't be turned into a clean error page,
// but it can at least be logged).
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("template execute failed", "template", name, "error", err, "path", r.URL.Path)
	}
}

// isHTMX reports whether the request was made by htmx (hx-get/hx-post), so a
// handler can choose to return just a partial instead of a full page.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
