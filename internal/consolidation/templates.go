package consolidation

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// templateFS embeds this module's own read-only/CRUD pages (pending list,
// wallet picker, address book). These are ordinary html/template pages —
// none of them touch mnemonics/private keys, so no strict-CSP offline-
// bundle treatment (that's Part 3's /admin/consolidation/sign page).
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{"amt": formatMicroAmount}).ParseFS(templateFS, "templates/*.html"))

// formatMicroAmount converts a stored smallest-unit integer (6 decimals —
// true for both USDT-TRC20 and TRX's sun) into a human-readable decimal
// string for display, trimming trailing zeros (30000000 -> "30",
// 30500000 -> "30.5"). Display only — CLAUDE.md 安全鐵律6's int64-only
// storage/arithmetic rule is untouched by this. Duplicated in
// internal/admin (same one-line-worth of logic, no shared owner package
// between the two — matches this codebase's existing precedent for small
// utilities, e.g. signCSPHeader).
func formatMicroAmount(amount int64) string {
	whole := amount / 1_000_000
	frac := amount % 1_000_000
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	fracStr := strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
	return fmt.Sprintf("%d.%s", whole, fracStr)
}

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("consolidation: render %s: %v", name, err)
	}
}
