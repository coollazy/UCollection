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

// totpReverifiedNotice returns a flash message explaining why the operator
// landed back on this GET page instead of their action completing — see
// auth.RequireTOTPCode's doc comment: GET-protected pages always bounce
// through /admin/reverify-totp first (nothing lost, just re-explains why),
// while POST/DELETE forms carry their own totp_code field and only bounce
// back here if that code was missing/wrong/replayed (?totp_error=...).
func totpReverifiedNotice(r *http.Request) string {
	switch r.URL.Query().Get("totp_error") {
	case "missing":
		return "此操作需要輸入TOTP驗證碼，請重新填寫並送出"
	case "invalid":
		return "TOTP驗證碼錯誤，請重新填寫並送出"
	case "replay":
		return "此驗證碼已被使用，請等待新一組驗證碼後再試"
	}
	if r.URL.Query().Get("totp_reverified") != "1" {
		return ""
	}
	return "TOTP已重新驗證，請重新填寫並送出剛才的操作"
}

// firstNonEmpty returns a, or b if a is empty. Used to let a page's own
// "?success=" flash take priority over the generic totpReverifiedNotice
// when (in principle) both could apply.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
