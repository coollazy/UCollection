package consolidation

import (
	"embed"
	"html/template"
	"log"
	"net/http"
)

// templateFS embeds this module's own read-only/CRUD pages (pending list,
// wallet picker, address book). These are ordinary html/template pages —
// none of them touch mnemonics/private keys, so no strict-CSP offline-
// bundle treatment (that's Part 3's /admin/consolidation/sign page).
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("consolidation: render %s: %v", name, err)
	}
}
