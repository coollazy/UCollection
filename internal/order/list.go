package order

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// ListFilter narrows down ListOrders (技術架構設計第11節「訂單列表與明細」：狀態
// （可複選）、merchant_order_no（模糊查詢）、address（精確比對）、代收主錢包、建立
// 時間範圍). All fields are optional (zero value = no filter on that column).
type ListFilter struct {
	Statuses        []Status
	MerchantOrderNo string
	Address         string
	MasterWalletID  *int64
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	Offset          int
	Limit           int
}

// ListOrders returns a page of orders matching f, newest first (技術架構設計
// 第11節：預設依created_at降冪排序，offset+limit分頁), plus the total row count
// across all pages (computed in the same query via COUNT(*) OVER(), so
// paginating doesn't cost a second round trip).
func ListOrders(ctx context.Context, pool *store.Pool, f ListFilter) ([]Order, int, error) {
	var where []string
	var args []any

	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if len(f.Statuses) > 0 {
		where = append(where, "status = ANY("+arg(statusStrings(f.Statuses))+")")
	}
	if f.MerchantOrderNo != "" {
		where = append(where, "merchant_order_no ILIKE "+arg("%"+f.MerchantOrderNo+"%"))
	}
	if f.Address != "" {
		where = append(where, "address = "+arg(f.Address))
	}
	if f.MasterWalletID != nil {
		where = append(where, "master_wallet_id = "+arg(*f.MasterWalletID))
	}
	if f.CreatedFrom != nil {
		where = append(where, "created_at >= "+arg(*f.CreatedFrom))
	}
	if f.CreatedTo != nil {
		where = append(where, "created_at <= "+arg(*f.CreatedTo))
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
		SELECT %s, COUNT(*) OVER() AS total
		FROM orders
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s OFFSET %s
	`, orderColumns, whereClause, arg(limit), arg(f.Offset))

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Order
	var total int
	for rows.Next() {
		o, err := scanOrderWithTotal(rows, &total)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func statusStrings(statuses []Status) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}

// scanOrderWithTotal is scanOrder plus one extra trailing COUNT(*) OVER()
// column — kept separate from scanOrder (order.go) since that helper is
// also used by callers whose SELECT has no window function.
func scanOrderWithTotal(row rowScanner, total *int) (Order, error) {
	var o Order
	err := row.Scan(
		&o.ID, &o.MerchantOrderNo, &o.PublicToken, &o.MasterWalletID, &o.DerivationIndex, &o.Address,
		&o.Status, &o.TargetAmount, &o.AmountLowerBound, &o.AmountUpperBound,
		&o.ValiditySeconds, &o.AmountTolerancePercent, &o.ConfirmationStallTimeoutSeconds,
		&o.ExpiresAt, &o.ConfirmingAt, &o.ConsolidationStatus, &o.CreatedAt, &o.UpdatedAt,
		total,
	)
	return o, err
}

// IncomingTransaction mirrors one incoming_transactions row (技術架構設計第11
// 節「訂單明細頁」：incoming_transactions列表).
type IncomingTransaction struct {
	ID          int64
	TxHash      string
	LogIndex    int
	Amount      int64
	Confirmed   bool
	BlockNumber int64
	CreatedAt   time.Time
}

// ListIncomingTransactions returns every incoming_transactions row for
// orderID, oldest first.
func ListIncomingTransactions(ctx context.Context, pool *store.Pool, orderID int64) ([]IncomingTransaction, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, tx_hash, log_index, amount, confirmed, block_number, created_at
		FROM incoming_transactions
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IncomingTransaction
	for rows.Next() {
		var t IncomingTransaction
		if err := rows.Scan(&t.ID, &t.TxHash, &t.LogIndex, &t.Amount, &t.Confirmed, &t.BlockNumber, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
