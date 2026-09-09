package admin

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/xuri/excelize/v2"
)

// exportPageSize is the streaming batch size — large enough to keep round
// trips reasonable, small enough that memory use stays bounded regardless
// of how many orders match the filter (技術架構設計第11節「逐列串流寫入...避免
// 記憶體壓力」).
const exportPageSize = 1000

// exportMaxRows is the hard cap on a single export (技術架構設計第11節：「單次
// 匯出上限10萬筆：超過此筆數，回傳明確錯誤要求商戶先縮小篩選範圍」).
const exportMaxRows = 100000

// exceedsExportCap is split out from exportOrdersHandler purely so a test
// can exercise the >10萬 boundary without seeding 100,000+ real rows.
func exceedsExportCap(total int) bool { return total > exportMaxRows }

var exportColumnHeaders = []string{
	"訂單ID", "merchant_order_no", "地址", "代收主錢包ID", "目標金額(USDT)", "已確認累計金額(USDT)", "狀態", "建立時間", "完成時間",
}

// exportOrdersHandler implements GET /admin/export/orders?format=csv|xlsx
// (技術架構設計第11節「CSV/Excel交易明細匯出」). Reuses the exact same filter
// parsing as GET /admin/orders (parseOrderListFilter, order_filter.go) —
// "可套用與/admin/orders相同的篩選條件，避免另建一套篩選邏輯" is implemented
// literally, not just in spirit. Streams by looping order.ListOrders with a
// large fixed page size rather than refactoring list.go to bypass its
// built-in LIMIT/COUNT(*) OVER() — see .claude/plans/polished-booping-
// kernighan.md「判斷點1」for why: lower risk to an already-shipped module,
// the extra per-page COUNT(*) OVER() recompute is negligible at the 10萬-row
// cap this endpoint enforces.
func exportOrdersHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "csv"
		}
		if format != "csv" && format != "xlsx" {
			http.Error(w, "invalid format, must be csv or xlsx", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		f := parseOrderListFilter(r)
		f.Offset = 0
		f.Limit = exportPageSize

		firstPage, total, err := order.ListOrders(ctx, deps.Pool, f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if exceedsExportCap(total) {
			http.Error(w, fmt.Sprintf("符合條件的訂單筆數(%d)超過匯出上限%d筆，請縮小篩選範圍後再匯出", total, exportMaxRows), http.StatusBadRequest)
			return
		}

		switch format {
		case "csv":
			exportOrdersCSV(ctx, w, deps.Pool, f, firstPage)
		case "xlsx":
			exportOrdersXLSX(ctx, w, deps.Pool, f, firstPage)
		}
	}
}

// exportRow is one flattened output row — TargetAmount/ConfirmedAmount stay
// int64 minimum-unit integers all the way to the writer (CLAUDE.md安全鐵律6),
// formatted to a human-readable USDT decimal string only at the very last
// step in each writer (formatMicroAmount, same helper the HTML templates use).
type exportRow struct {
	OrderID         int64
	MerchantOrderNo string
	Address         string
	MasterWalletID  int64
	TargetAmount    int64
	ConfirmedAmount int64
	Status          string
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

// streamExportRows pages through order.ListOrders starting from firstPage
// (already fetched by the caller to check the 10萬-row cap before committing
// to a response format), batch-loading confirmed amounts and completion
// times per page (order.SumConfirmedAmountsForOrders/LatestTransitionTimes)
// instead of one query per order, and calls yield once per row in order.
// CompletedAt is only ever set for terminal-status orders (技術架構設計原文
// only says「建立/完成時間」without a precise definition — this implementation
// reads it as「訂單目前為終態時，order_state_transitions裡該訂單最後一筆的
// created_at」，見計畫檔案「判斷點3」).
func streamExportRows(ctx context.Context, pool *store.Pool, f order.ListFilter, firstPage []order.Order, yield func(exportRow) error) error {
	page := firstPage
	for {
		if len(page) == 0 {
			return nil
		}

		orderIDs := make([]int64, len(page))
		for i, o := range page {
			orderIDs[i] = o.ID
		}
		sums, err := order.SumConfirmedAmountsForOrders(ctx, pool, orderIDs)
		if err != nil {
			return err
		}
		completions, err := order.LatestTransitionTimes(ctx, pool, orderIDs)
		if err != nil {
			return err
		}

		for _, o := range page {
			row := exportRow{
				OrderID:         o.ID,
				MerchantOrderNo: o.MerchantOrderNo,
				Address:         o.Address,
				MasterWalletID:  o.MasterWalletID,
				TargetAmount:    o.TargetAmount,
				ConfirmedAmount: sums[o.ID],
				Status:          string(o.Status),
				CreatedAt:       o.CreatedAt,
			}
			if order.IsTerminalStatus(o.Status) {
				if at, ok := completions[o.ID]; ok {
					row.CompletedAt = &at
				}
			}
			if err := yield(row); err != nil {
				return err
			}
		}

		if len(page) < exportPageSize {
			return nil
		}
		f.Offset += exportPageSize
		page, _, err = order.ListOrders(ctx, pool, f)
		if err != nil {
			return err
		}
	}
}

func formatExportTime(t time.Time) string { return t.Format(time.RFC3339) }

func rowToStrings(row exportRow) []string {
	completed := ""
	if row.CompletedAt != nil {
		completed = formatExportTime(*row.CompletedAt)
	}
	return []string{
		strconv.FormatInt(row.OrderID, 10),
		row.MerchantOrderNo,
		row.Address,
		strconv.FormatInt(row.MasterWalletID, 10),
		formatMicroAmount(row.TargetAmount),
		formatMicroAmount(row.ConfirmedAmount),
		row.Status,
		formatExportTime(row.CreatedAt),
		completed,
	}
}

// exportOrdersCSV streams stdlib encoding/csv straight to w, UTF-8 with a
// leading BOM so Excel opens Chinese headers without mangling them (技術架構
// 設計第11節).
func exportOrdersCSV(ctx context.Context, w http.ResponseWriter, pool *store.Pool, f order.ListFilter, firstPage []order.Order) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		log.Printf("admin: export csv: write BOM: %v", err)
		return
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(exportColumnHeaders); err != nil {
		log.Printf("admin: export csv: write header: %v", err)
		return
	}

	err := streamExportRows(ctx, pool, f, firstPage, func(row exportRow) error {
		return cw.Write(rowToStrings(row))
	})
	if err != nil {
		log.Printf("admin: export csv: stream rows: %v", err)
	}
	cw.Flush()
}

// exportOrdersXLSX builds the workbook with excelize's StreamWriter (spills
// to a temp file past 16MB of in-memory row data, per its own doc comment —
// this is what keeps a 10萬-row export from holding everything in RAM, the
// same goal CSV achieves by writing straight to w) then writes the finished
// file to w in one shot — unlike CSV, the zip-based xlsx container format
// can't be incrementally flushed to an HTTP response mid-construction.
func exportOrdersXLSX(ctx context.Context, w http.ResponseWriter, pool *store.Pool, f order.ListFilter, firstPage []order.Order) {
	xf := excelize.NewFile()
	defer func() { _ = xf.Close() }()

	sw, err := xf.NewStreamWriter("Sheet1")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	headerRow := make([]any, len(exportColumnHeaders))
	for i, h := range exportColumnHeaders {
		headerRow[i] = h
	}
	if err := sw.SetRow("A1", headerRow); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rowNum := 2
	err = streamExportRows(ctx, pool, f, firstPage, func(row exportRow) error {
		cell, err := excelize.CoordinatesToCellName(1, rowNum)
		if err != nil {
			return err
		}
		rowNum++
		completed := ""
		if row.CompletedAt != nil {
			completed = formatExportTime(*row.CompletedAt)
		}
		return sw.SetRow(cell, []any{
			row.OrderID, row.MerchantOrderNo, row.Address, row.MasterWalletID,
			formatMicroAmount(row.TargetAmount), formatMicroAmount(row.ConfirmedAmount), row.Status, formatExportTime(row.CreatedAt), completed,
		})
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := sw.Flush(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.xlsx"`)
	if err := xf.Write(w); err != nil {
		log.Printf("admin: export xlsx: write response: %v", err)
	}
}
