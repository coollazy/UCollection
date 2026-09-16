package admin

import (
	"net/url"
	"strconv"
)

// parsePage reads the requested page number from either "page" (set by the
// Prev/Next buttons) or "goto_page" (set by the page-jump input) — every
// pager form on this site carries both fields at once, so a Prev/Next click
// always wins over whatever stale number happens to be sitting in the jump
// box, and the jump box works when neither Prev nor Next was the trigger
// (見 orders_list.html/audit_logs.html/notifications.html 的pager表單).
func parsePage(q url.Values) int {
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		return p
	}
	if p, err := strconv.Atoi(q.Get("goto_page")); err == nil && p > 0 {
		return p
	}
	return 1
}
