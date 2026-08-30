package httpserver

import (
	"net/http"
)

type pageData struct {
	Base       string
	ActivePage string
	Flash      string
	FlashKind  string
}

func (s *Server) newPageData(active string) pageData {
	return pageData{Base: s.base(), ActivePage: active}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("template execute failed", "template", name, "error", err, "path", r.URL.Path)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
