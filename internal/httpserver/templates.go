package httpserver

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// parseTemplates parses every embedded template into one *template.Template,
// so any {{define}} block can reference any other regardless of which file
// it lives in (base.html's layout blocks, page bodies, and the htmx partials
// all share one namespace).
func parseTemplates() (*template.Template, error) {
	return template.New("root").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html")
}
