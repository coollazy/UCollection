package admin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// describeAuditLog translates one audit_logs row into a human-readable
// Traditional Chinese sentence for /admin/audit-logs (驗收問題第4項：detail欄
// 原本直接印`k=v k=v`工程師取向鍵值對，商戶看不懂). Display only — audit.Log's
// stored detail JSON, and the raw action_type column used by the page's
// filter form, are both untouched.
//
// action_type is a plain string with no enum/versioning, so a row whose
// type or detail shape isn't recognized here (e.g. a future action_type
// nobody's added a case for yet) falls back to rawDetail's `k=v k=v` dump
// instead of erroring the whole page.
func describeAuditLog(e auditLogEntryView) string {
	d := e.Detail

	switch e.ActionType {
	case "LOGIN_SUCCESS":
		return fmt.Sprintf("登入成功（IP：%s）", detailStr(d, "ip"))
	case "LOGIN_FAILED":
		return fmt.Sprintf("登入失敗，帳號或密碼錯誤（IP：%s）", detailStr(d, "ip"))
	case "LOGOUT":
		return "登出"
	case "PASSWORD_CHANGED":
		return "變更登入密碼"
	case "TOTP_SETUP_SUCCESS":
		return fmt.Sprintf("完成兩步驟驗證（TOTP）設定（IP：%s）", detailStr(d, "ip"))
	case "TOTP_SETUP_FAILED":
		return fmt.Sprintf("兩步驟驗證（TOTP）設定失敗，驗證碼錯誤（IP：%s）", detailStr(d, "ip"))
	case "TOTP_VERIFY_SUCCESS":
		return fmt.Sprintf("登入時TOTP驗證碼驗證成功（IP：%s）", detailStr(d, "ip"))
	case "TOTP_VERIFY_FAILED":
		return fmt.Sprintf("登入時TOTP驗證碼錯誤（IP：%s）", detailStr(d, "ip"))
	case "TOTP_STEPUP_SUCCESS":
		return fmt.Sprintf("高風險操作前的TOTP驗證成功（IP：%s）", detailStr(d, "ip"))
	case "TOTP_STEPUP_FAILED":
		return fmt.Sprintf("高風險操作前的TOTP驗證失敗（IP：%s）", detailStr(d, "ip"))
	case "TOTP_REVERIFY_SUCCESS":
		return fmt.Sprintf("重新驗證TOTP成功（IP：%s）", detailStr(d, "ip"))
	case "TOTP_REVERIFY_FAILED":
		return fmt.Sprintf("重新驗證TOTP失敗（IP：%s）", detailStr(d, "ip"))
	case "SESSION_LOCKED":
		return fmt.Sprintf("TOTP連續驗證失敗次數過多，帳號暫時鎖定（IP：%s）", detailStr(d, "ip"))

	case "PARAMS_CHANGED":
		return fmt.Sprintf("更新系統參數：訂單有效期 %d 秒、確認延遲逾時 %d 秒（僅影響之後新建立的訂單）",
			detailInt64(d, "validity_seconds"), detailInt64(d, "confirmation_stall_timeout_seconds"))
	case "AMOUNT_TOLERANCE_CHANGED":
		return fmt.Sprintf("調整金額容許誤差為 %s%%（僅影響之後新建立的訂單）", trimFloat(detailFloat64(d, "amount_tolerance_percent")))

	case "ORDER_MANUAL_CREATED":
		return fmt.Sprintf("手動建立訂單（商戶訂單編號：%s，目標金額：%s USDT）",
			detailStr(d, "merchant_order_no"), formatMicroAmount(detailInt64(d, "target_amount")))

	case "ILLEGAL_STATE_TRANSITION":
		if e.Actor == "system" {
			return fmt.Sprintf("訂單（目前狀態 %s）於終態後又收到入帳，金額 %s USDT（交易 %s），系統未自動處置，請人工查證",
				detailStr(d, "order_status"), formatMicroAmount(detailInt64(d, "event_amount")), detailStr(d, "tx_hash"))
		}
		return fmt.Sprintf("嘗試將訂單改判為 %s 被系統拒絕（不允許的狀態轉換），操作理由：%s",
			detailStr(d, "to_status"), detailStr(d, "note"))
	case "ORDER_MANUAL_OVERRIDE":
		return fmt.Sprintf("手動將訂單改判為 %s，理由：%s", detailStr(d, "to_status"), detailStr(d, "note"))
	case "ORDER_MANUAL_REVERIFY":
		if errMsg := detailStr(d, "error"); errMsg != "" {
			return fmt.Sprintf("手動重新查證訂單地址 %s 失敗：%s", detailStr(d, "address"), errMsg)
		}
		return fmt.Sprintf("手動重新查證訂單地址 %s", detailStr(d, "address"))

	case "MASTER_WALLET_CREATED":
		return "新增並啟用代收主錢包"
	case "MASTER_WALLET_REACTIVATED":
		return "重新啟用代收主錢包"

	case "WEBHOOK_URL_CHANGED":
		return fmt.Sprintf("變更Webhook URL為：%s", detailStr(d, "webhook_url"))
	case "WEBHOOK_SECRET_ROTATED":
		mode := "手動輸入"
		if detailStr(d, "mode") == "generate" {
			mode = "系統產生"
		}
		return fmt.Sprintf("輪替Webhook secret（%s）", mode)

	case "API_KEY_REGENERATED":
		return "重新產生API Key（舊key即刻失效）"

	case "NOTIFICATION_MANUAL_RESEND":
		if errMsg := detailStr(d, "error"); errMsg != "" {
			return fmt.Sprintf("手動重發Webhook通知失敗：%s", errMsg)
		}
		return "手動重發Webhook通知"

	case "CONSOLIDATION":
		return describeConsolidationOrFeeTopup(d, "歸集", "USDT")
	case "FEE_TOPUP":
		return describeConsolidationOrFeeTopup(d, "TRX手續費補充", "TRX")

	case "FEE_SOURCE_BOOK_ENTRY_CREATED":
		return fmt.Sprintf("新增手續費來源地址簿項目：%s（%s）", detailStr(d, "label"), detailStr(d, "address"))
	case "FEE_SOURCE_BOOK_ENTRY_RENAMED":
		return fmt.Sprintf("重新命名手續費來源地址簿項目為：%s", detailStr(d, "label"))
	case "FEE_SOURCE_BOOK_ENTRY_DELETED":
		return "刪除手續費來源地址簿項目"
	case "ADDRESS_BOOK_ENTRY_CREATED":
		return fmt.Sprintf("新增歸集地址簿項目：%s（%s）", detailStr(d, "label"), detailStr(d, "address"))
	case "ADDRESS_BOOK_ENTRY_RENAMED":
		return fmt.Sprintf("重新命名歸集地址簿項目為：%s", detailStr(d, "label"))
	case "ADDRESS_BOOK_ENTRY_DELETED":
		return "刪除歸集地址簿項目"

	case "CONSOLIDATION_BATCH_CREATED":
		return fmt.Sprintf("建立歸集批次，目的地 %s，涵蓋訂單 #%s",
			detailStr(d, "destination_address"), detailIDList(d, "order_ids"))
	case "FEE_TOPUP_BATCH_CREATED":
		return fmt.Sprintf("建立TRX手續費補充批次，來源 %s（%s），每筆 %s TRX，涵蓋訂單 #%s",
			detailStr(d, "fee_source"), detailStr(d, "fee_source_address"),
			formatMicroAmount(detailInt64(d, "amount_per_order")), detailIDList(d, "order_ids"))
	case "CONSOLIDATION_FLOW_CREATED":
		return fmt.Sprintf("建立整合歸集流程，目的地 %s，手續費來源 %s（%s），首筆 %s TRX／其餘每筆 %s TRX，涵蓋訂單 #%s",
			detailStr(d, "destination_address"), detailStr(d, "fee_source"), detailStr(d, "fee_source_address"),
			formatMicroAmount(detailInt64(d, "first_amount_sun")), formatMicroAmount(detailInt64(d, "repeat_amount_sun")),
			detailIDList(d, "order_ids"))

	case "SCAN_CHECKPOINT_ANOMALY":
		return fmt.Sprintf("掃描檢查點嘗試倒退：%s 欄位目前值 %d，嘗試寫入 %d（已自動忽略，維持原值）",
			detailStr(d, "column"), detailInt64(d, "current"), detailInt64(d, "attempted"))
	}

	return rawDetail(d)
}

// describeConsolidationOrFeeTopup covers CONSOLIDATION/FEE_TOPUP, which each
// share one action_type across two different life-cycle stages
// ("prepared"/"broadcast" — see internal/consolidation/prepare.go and
// broadcast.go) distinguished only by detail["stage"].
func describeConsolidationOrFeeTopup(d map[string]any, verb, unit string) string {
	orderRef := fmt.Sprintf("訂單#%d", detailInt64(d, "order_id"))
	switch detailStr(d, "stage") {
	case "prepared":
		return fmt.Sprintf("準備%s（%s，批次#%d），金額 %s %s，交易 %s",
			verb, orderRef, detailInt64(d, "batch_id"), formatMicroAmount(detailInt64(d, "amount")), unit, detailStr(d, "tx_hash"))
	case "broadcast":
		status := detailStr(d, "status")
		msg := fmt.Sprintf("廣播%s交易（%s），結果：%s（交易 %s）", verb, orderRef, status, detailStr(d, "tx_hash"))
		if status != "success" {
			if errDetail := detailStr(d, "error_detail"); errDetail != "" {
				msg += "，失敗原因：" + errDetail
			}
		}
		return msg
	}
	return rawDetail(d)
}

// detailStr/detailInt64/detailFloat64/detailIDList read audit_logs.detail
// (jsonb decoded into map[string]any — see internal/audit/list.go's Entry).
// JSON numbers always decode to float64 regardless of the int64 Go value
// audit.Log was originally called with, so the numeric accessors convert
// back; missing/wrong-shaped keys just return the zero value rather than
// panicking, since detail's shape is defined by call-site convention, not
// an enforced schema.
func detailStr(d map[string]any, key string) string {
	s, _ := d[key].(string)
	return s
}

func detailInt64(d map[string]any, key string) int64 {
	switch v := d[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func detailFloat64(d map[string]any, key string) float64 {
	f, _ := d[key].(float64)
	return f
}

// detailIDList renders a JSON array detail value (e.g. order_ids) as a
// "、"-joined string of its elements.
func detailIDList(d map[string]any, key string) string {
	raw, ok := d[key].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(raw))
	for _, v := range raw {
		if n, ok := v.(float64); ok {
			parts = append(parts, strconv.FormatInt(int64(n), 10))
			continue
		}
		parts = append(parts, fmt.Sprintf("%v", v))
	}
	return strings.Join(parts, "、")
}

// trimFloat formats a float64 without trailing zeros (10 -> "10", 12.5 ->
// "12.5"), matching formatMicroAmount's display convention for percentages.
func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", f), "0"), ".")
}

// rawDetail reproduces the page's original "k=v k=v" dump — the fallback
// for any action_type (present or added later) without a case above.
func rawDetail(d map[string]any) string {
	if len(d) == 0 {
		return ""
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, d[k]))
	}
	return strings.Join(parts, " ")
}
