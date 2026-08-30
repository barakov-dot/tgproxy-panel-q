package httpserver

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func parseTemplates() (*template.Template, error) {
	return template.New("root").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html")
}
