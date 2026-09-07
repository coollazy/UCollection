package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

const auditLogsPageSize = 50

// auditLogEntryView wraps audit.Entry with a plain bool the template can
// branch on — html/template's eq builtin doesn't dereference *string (it
// only unwraps interface{}, not real pointers), so comparing
// .TargetType == "order" straight in the template fails at render time.
// Precomputing the comparison here keeps that quirk out of the template.
type auditLogEntryView struct {
	audit.Entry
	IsOrderTarget bool
}

type auditLogsListPageData struct {
	Entries    []auditLogEntryView
	Filter     audit.Filter
	Page       int
	TotalPages int
	Total      int
	PrevPage   int
	NextPage   int
	HasPrev    bool
	HasNext    bool
}

// auditLogsListHandler implements GET /admin/audit-logs (技術架構設計第11節
// 「稽核日誌查詢」：依actor、action_type、target_type、時間範圍篩選；detail(jsonb)
// 於明細展開顯示). Pagination mirrors orders_list.go's exact pattern.
func auditLogsListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		page := 1
		if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
			page = p
		}

		f := audit.Filter{
			Actor:      q.Get("actor"),
			ActionType: q.Get("action_type"),
			TargetType: q.Get("target_type"),
			Offset:     (page - 1) * auditLogsPageSize,
			Limit:      auditLogsPageSize,
		}
		if v := q.Get("created_from"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				f.CreatedFrom = t
			}
		}
		if v := q.Get("created_to"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				f.CreatedTo = t.Add(24 * time.Hour)
			}
		}

		entries, total, err := audit.List(r.Context(), deps.Pool, f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + auditLogsPageSize - 1) / auditLogsPageSize
		if totalPages == 0 {
			totalPages = 1
		}

		views := make([]auditLogEntryView, len(entries))
		for i, e := range entries {
			views[i] = auditLogEntryView{Entry: e, IsOrderTarget: e.TargetType != nil && *e.TargetType == "order"}
		}

		render(w, http.StatusOK, "audit_logs.html", auditLogsListPageData{
			Entries:    views,
			Filter:     f,
			Page:       page,
			TotalPages: totalPages,
			Total:      total,
			PrevPage:   page - 1,
			NextPage:   page + 1,
			HasPrev:    page > 1,
			HasNext:    page < totalPages,
		})
	}
}
