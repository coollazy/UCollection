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

var templates = template.Must(template.New("").Funcs(template.FuncMap{"amt": formatMicroAmount}).ParseFS(templateFS, "templates/*.html"))

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
