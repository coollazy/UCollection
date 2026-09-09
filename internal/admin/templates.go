package admin

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

// templateFS embeds this Part's pages (dashboard, order list/detail/new).
// None of these touch mnemonics/private keys, so no strict-CSP
// offline-bundle treatment is needed (compare
// internal/consolidation/templates/consolidation_sign.html).
//
//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{"amt": formatMicroAmount, "ts": formatTimestamp}).ParseFS(templateFS, "templates/*.html"))

// formatMicroAmount converts a stored smallest-unit integer (6 decimals —
// true for both USDT-TRC20 and TRX's sun) into a human-readable decimal
// string for display, trimming trailing zeros (30000000 -> "30",
// 30500000 -> "30.5"). Display only — CLAUDE.md 安全鐵律6's int64-only
// storage/arithmetic rule is untouched by this; nothing upstream of a
// template call site changes type or value.
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
// doesn't run. nil *time.Time (e.g. NextRetryAt, LastSyncedAt) renders as
// empty, so callers don't need a separate {{if}} guard.
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
