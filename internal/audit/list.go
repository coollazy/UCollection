package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// Filter narrows down List (技術架構設計第11節「稽核日誌查詢」：可依actor、
// action_type、target_type、時間範圍篩選). All fields are optional (zero
// value = no filter on that column).
type Filter struct {
	Actor                  string
	ActionType             string
	TargetType             string
	CreatedFrom, CreatedTo time.Time
	Offset, Limit          int
}

// Entry is one audit_logs row, detail decoded from JSONB into a plain map
// for template rendering (nil when the row's detail column is SQL NULL —
// Log() stores nil detail as NULL, not the JSON literal "null").
type Entry struct {
	ID         int64
	Actor      string
	ActionType string
	TargetType *string
	TargetID   *int64
	Detail     map[string]any
	CreatedAt  time.Time
}

// List returns a page of audit_logs rows matching f, newest first, plus the
// total row count across all pages (技術架構設計第11節：同一份WHERE組裝＋
// COUNT(*) OVER()分頁模式，逐字比照internal/order/list.go的ListOrders，本套件
// 至今只有Log()寫入side，這是第一個讀取side).
func List(ctx context.Context, pool *store.Pool, f Filter) ([]Entry, int, error) {
	var where []string
	var args []any

	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if f.Actor != "" {
		where = append(where, "actor = "+arg(f.Actor))
	}
	if f.ActionType != "" {
		where = append(where, "action_type = "+arg(f.ActionType))
	}
	if f.TargetType != "" {
		where = append(where, "target_type = "+arg(f.TargetType))
	}
	if !f.CreatedFrom.IsZero() {
		where = append(where, "created_at >= "+arg(f.CreatedFrom))
	}
	if !f.CreatedTo.IsZero() {
		where = append(where, "created_at <= "+arg(f.CreatedTo))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT id, actor, action_type, target_type, target_id, detail, created_at, COUNT(*) OVER() AS total
		FROM audit_logs
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s OFFSET %s
	`, whereClause, arg(limit), arg(f.Offset))

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Entry
	var total int
	for rows.Next() {
		var e Entry
		var detailJSON []byte
		if err := rows.Scan(&e.ID, &e.Actor, &e.ActionType, &e.TargetType, &e.TargetID, &detailJSON, &e.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		if detailJSON != nil {
			if err := json.Unmarshal(detailJSON, &e.Detail); err != nil {
				return nil, 0, fmt.Errorf("audit: list: unmarshal detail for entry %d: %w", e.ID, err)
			}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
