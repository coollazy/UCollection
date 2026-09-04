package auth

import (
	"embed"
	"html/template"
	"log"
	"net/http"
)

// templateFS embeds auth's own minimal, unstyled form pages (login, TOTP
// setup/verify/reverify, password change). These are ordinary
// html/template pages, not the strict-CSP offline-bundle treatment
// reserved for pages that handle mnemonics/private keys (技術架構設計第9節/
// 第10節) — none of these do. Polished layout/navigation is left to
// internal/admin.
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("auth: render %s: %v", name, err)
	}
}
