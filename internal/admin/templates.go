package admin

import (
	"embed"
	"html/template"
	"log"
	"net/http"
)

// templateFS embeds this Part's pages (dashboard, order list/detail/new).
// None of these touch mnemonics/private keys, so no strict-CSP
// offline-bundle treatment is needed (compare
// internal/consolidation/templates/consolidation_sign.html).
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("admin: render %s: %v", name, err)
	}
}
