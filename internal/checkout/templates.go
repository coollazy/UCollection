package checkout

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// templateFS embeds this module's public payment page. These pages never
// touch mnemonics/private keys (見技術架構設計第6節「安全性」), so they get a
// baseline CSP rather than the strict offline-bundle treatment of the
// /admin/consolidation/sign page.
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{"amt": formatMicroAmount, "pillClass": pillClass}).ParseFS(templateFS, "templates/*.html"))

// pillClasses maps this package's order-status values to the admin.css pill
// modifier class carrying its semantic color (見 admin.css 第7節). Duplicated
// from internal/admin's pillClasses (same "no shared owner package between
// modules" precedent as formatMicroAmount above) — trimmed to only the 6
// order.Status values this page ever displays, since checkout never shows
// consolidation/webhook/master-wallet statuses.
var pillClasses = map[string]string{
	"PENDING":              "pill--pending",
	"CONFIRMING":           "pill--confirming",
	"COMPLETED":            "pill--completed",
	"OVERPAID":             "pill--overpaid",
	"EXPIRED":              "pill--expired",
	"CONFIRMATION_STALLED": "pill--stalled",
}

// pillClass looks up the admin.css pill modifier class for status. Unlike
// internal/admin's statusPill, this only returns the class (not a rendered
// <span>) — status_region.html displays the human-readable .Label
// ("等待付款" 等) as the pill text, not the raw status value, so the template
// composes the <span> itself. An unrecognized value (not expected in
// practice) returns "", which renders as a bare, colorless `.pill`.
func pillClass(status string) string {
	return pillClasses[status]
}

// formatMicroAmount converts a stored smallest-unit integer (USDT-TRC20 has
// 6 decimals) into a human-readable decimal string, trimming trailing zeros
// (30000000 -> "30", 30500000 -> "30.5"). Display only — CLAUDE.md 安全鐵律6's
// int64-only storage/arithmetic rule is untouched. Duplicated from
// internal/admin & internal/consolidation (same one-line-worth of logic, no
// shared owner package between modules — matches this codebase's existing
// precedent for small utilities).
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
		log.Printf("checkout: render %s: %v", name, err)
	}
}
