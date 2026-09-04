package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/order"
)

// generateManualOrderNo produces MANUAL-{yyyyMMddHHmmss}-{4碼隨機} (技術架構設計
// 第11節「手動建單」：「自動編號須於表單頁面首次載入當下產生並以隱藏欄位隨表單送出，而非
// 於送出當下才產生」——called once per GET /admin/orders render, embedded as a
// hidden field in orders_list.html's manual-create form).
func generateManualOrderNo() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	digits := (int(buf[0])<<8 | int(buf[1])) % 10000
	return fmt.Sprintf("MANUAL-%s-%04d", time.Now().Format("20060102150405"), digits)
}

// manualCreateSubmitHandler implements POST /admin/orders (技術架構設計第11節
// 「手動建單」). RequireMasterWallet Gate 內嵌在這裡（不是獨立middleware）：無使用中
// 代收主錢包時導回訂單列表頁並帶明確錯誤訊息，不是500。
func manualCreateSubmitHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		masterWalletID, err := order.ActiveMasterWalletID(ctx, deps.Pool)
		if errors.Is(err, order.ErrNoActiveMasterWallet) {
			redirectOrdersListError(w, r, "尚無使用中的代收主錢包，請先於代收主錢包設定頁新增")
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		merchantOrderNo := r.PostForm.Get("merchant_order_no")
		if merchantOrderNo == "" {
			merchantOrderNo = generateManualOrderNo()
		}

		targetAmount, err := parseUSDTAmount(r.PostForm.Get("target_amount"))
		if err != nil {
			redirectOrdersListError(w, r, "金額格式錯誤："+err.Error())
			return
		}

		validitySeconds, tolerancePercent, stallTimeoutSeconds, err := loadOrderParamDefaults(ctx, deps)
		if err != nil {
			redirectOrdersListError(w, r, err.Error())
			return
		}

		ord, err := order.CreateOrder(ctx, deps.Pool, order.CreateParams{
			MerchantOrderNo:                 merchantOrderNo,
			MasterWalletID:                  masterWalletID,
			TargetAmount:                    targetAmount,
			ValiditySeconds:                 validitySeconds,
			AmountTolerancePercent:          tolerancePercent,
			ConfirmationStallTimeoutSeconds: stallTimeoutSeconds,
		})
		if errors.Is(err, order.ErrDuplicateMerchantOrderNo) {
			existing, lookupErr := order.GetByMerchantOrderNo(ctx, deps.Pool, merchantOrderNo)
			if lookupErr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if existing.TargetAmount == targetAmount {
				// 冪等：金額相同視為同一次提交（例如重整頁面重送表單），直接導向既有訂單
				// （技術架構設計第11節「冪等性判斷邏輯與第7節API共用同一段程式碼」）。
				http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash=order_exists", existing.ID), http.StatusSeeOther)
				return
			}
			redirectOrdersListError(w, r, fmt.Sprintf("merchant_order_no %q 已存在但金額不同", merchantOrderNo))
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		targetType := "order"
		_ = audit.Log(ctx, deps.Pool, "admin", "ORDER_MANUAL_CREATED", &targetType, &ord.ID, map[string]any{
			"merchant_order_no": ord.MerchantOrderNo,
			"target_amount":     ord.TargetAmount,
		})

		http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash=order_created", ord.ID), http.StatusSeeOther)
	}
}

func redirectOrdersListError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/admin/orders?flash_error="+url.QueryEscape(msg), http.StatusSeeOther)
}

// loadOrderParamDefaults reads the three order-creation parameters this
// module shares with internal/api's建單API — sourced from system_params,
// same PARAMS_NOT_CONFIGURED-shaped guard (技術架構設計第11節「手動建單」：「與第7節
// 建單API共用同一內部訂單建立函式...不重複實作HD衍生/參數快照邏輯」).
func loadOrderParamDefaults(ctx context.Context, deps Deps) (validitySeconds int64, tolerancePercent float64, stallTimeoutSeconds int64, err error) {
	var validity, stallTimeout *int64
	var tolerance *float64
	e := deps.Pool.QueryRow(ctx, `
		SELECT validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds
		FROM system_params WHERE id = 1
	`).Scan(&validity, &tolerance, &stallTimeout)
	if e != nil {
		return 0, 0, 0, e
	}
	if validity == nil || tolerance == nil || stallTimeout == nil {
		return 0, 0, 0, errUnconfiguredOrderParams
	}
	return *validity, *tolerance, *stallTimeout, nil
}

var errUnconfiguredOrderParams = errors.New("尚未於參數設定頁設定訂單有效期/金額容許誤差/確認等待逾時時間")

// parseUSDTAmount converts a human-entered decimal USDT string (as typed
// in the 手動建單 form, e.g. "100.50") into its int64 minimum-unit integer
// (USDT has 6 decimals). Rejects anything with more than 6 fractional
// digits outright — 技術架構設計第11節「手動建單」：「超過6位小數一律拒絕送出並提示
// 錯誤，不做四捨五入或截斷」（CLAUDE.md 安全鐵律6：金額全程int64整數運算）.
func parseUSDTAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("金額為必填")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("金額不可為負數")
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if hasFrac && len(fracPart) > 6 {
		return 0, fmt.Errorf("最多只能輸入6位小數")
	}
	fracPart += strings.Repeat("0", 6-len(fracPart))
	if intPart == "" {
		intPart = "0"
	}

	minorUnits, err := strconv.ParseInt(intPart+fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("無法辨識的金額格式")
	}
	if minorUnits <= 0 {
		return 0, fmt.Errorf("金額必須大於0")
	}
	return minorUnits, nil
}
