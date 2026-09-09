package consolidation

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// templateFS embeds this module's own read-only/CRUD pages (pending list,
// wallet picker, address book). These are ordinary html/template pages —
// none of them touch mnemonics/private keys, so no strict-CSP offline-
// bundle treatment (that's Part 3's /admin/consolidation/sign page).
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{"amt": formatMicroAmount, "ts": formatTimestamp}).ParseFS(templateFS, "templates/*.html"))

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

// formatTimestamp renders a stored time.Time/*time.Time as a <time
// datetime="..."> element: the datetime attribute carries UTC RFC3339 for
// web/static/js/localtime.js to convert to the browser's local timezone,
// and the element's text content is a readable UTC fallback for when JS
// doesn't run. nil *time.Time renders as empty, so callers don't need a
// separate {{if}} guard. Duplicated in internal/admin (same reasoning as
// formatMicroAmount's duplication above).
func formatTimestamp(t any) template.HTML {
	var tt time.Time
	switch v := t.(type) {
	case time.Time:
		tt = v
	case *time.Time:
		if v == nil {
			return ""
		}
		tt = *v
	default:
		return ""
	}
	return template.HTML(fmt.Sprintf( //nolint:gosec // content is server-formatted time.Time, not user input
		`<time datetime="%s">%s</time>`,
		tt.UTC().Format(time.RFC3339),
		tt.UTC().Format("2006-01-02 15:04:05 UTC"),
	))
}

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("consolidation: render %s: %v", name, err)
	}
}
