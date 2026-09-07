package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coollazy/UCollection/internal/audit"
)

type paramsPageData struct {
	ValiditySeconds        *int64
	AmountTolerancePercent *float64
	StallTimeoutSeconds    *int64
	FlashError             string
	FlashSuccess           string
}

func paramsErrorMessage(code string) string {
	switch code {
	case "invalid_validity_seconds":
		return "訂單有效期須為正整數（秒）"
	case "invalid_tolerance_percent":
		return "金額容許誤差百分比須為非負數"
	case "invalid_stall_timeout":
		return "確認等待逾時時間須為正整數（秒），或留空"
	case "bad_request":
		return "表單格式錯誤"
	default:
		return ""
	}
}

// paramsPageHandler implements GET /admin/params (技術架構設計第11節「參數設定」).
func paramsPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var validity, stallTimeout *int64
		var tolerance *float64
		err := deps.Pool.QueryRow(r.Context(), `
			SELECT validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds
			FROM system_params WHERE id = 1
		`).Scan(&validity, &tolerance, &stallTimeout)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		q := r.URL.Query()
		render(w, http.StatusOK, "params.html", paramsPageData{
			ValiditySeconds:        validity,
			AmountTolerancePercent: tolerance,
			StallTimeoutSeconds:    stallTimeout,
			FlashError:             paramsErrorMessage(q.Get("error")),
			FlashSuccess:           firstNonEmpty(q.Get("success"), totpReverifiedNotice(r)),
		})
	}
}

// updateParamsHandler implements POST /admin/params (技術架構設計第11節：僅影響之
// 後新建立的訂單；RequireSession即可). 只處理validity_seconds／
// confirmation_stall_timeout_seconds兩欄——amount_tolerance_percent已拆到
// updateToleranceHandler(POST /admin/params/tolerance，RequireFreshTOTP)，理由見
// 該handler註解，這裡刻意不再碰這個欄位，避免兩個handler互相覆蓋彼此的值
// （UPDATE只SET自己負責的欄位）。確認等待逾時時間表單留空時，比照畫面提示文字
// 「預設沿用訂單有效期」直接把validity_seconds的值寫入該欄位——而不是把它存成
// NULL：internal/admin/orders_manual_create.go的loadOrderParamDefaults（Part 1
// 既有、已上線的邏輯）把三欄視為「全部設定才算已設定」，若這裡存NULL會讓它整組被
// 判定成「尚未設定」而擋下所有手動建單/API建單，這不是本頁「留空」提示想要的效果
// ——UI上的「留空」只是操作便利，寫入DB時仍要有一個確定的數值。
func updateParamsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectParamsError(w, r, "bad_request")
			return
		}

		validitySeconds, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("validity_seconds")), 10, 64)
		if err != nil || validitySeconds <= 0 {
			redirectParamsError(w, r, "invalid_validity_seconds")
			return
		}

		stallTimeoutSeconds := validitySeconds
		if raw := strings.TrimSpace(r.PostFormValue("confirmation_stall_timeout_seconds")); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v <= 0 {
				redirectParamsError(w, r, "invalid_stall_timeout")
				return
			}
			stallTimeoutSeconds = v
		}

		ctx := r.Context()
		_, err = deps.Pool.Exec(ctx, `
			UPDATE system_params
			SET validity_seconds = $1, confirmation_stall_timeout_seconds = $2, updated_at = now()
			WHERE id = 1
		`, validitySeconds, stallTimeoutSeconds)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		_ = audit.Log(ctx, deps.Pool, "admin", "PARAMS_CHANGED", nil, nil, map[string]any{
			"validity_seconds":                   validitySeconds,
			"confirmation_stall_timeout_seconds": stallTimeoutSeconds,
		})

		http.Redirect(w, r, "/admin/params?success="+url.QueryEscape(successParamsUpdated), http.StatusSeeOther)
	}
}

// updateToleranceHandler implements POST /admin/params/tolerance
// (RequireFreshTOTP). 階段07全系統審查發現：amount_tolerance_percent原本跟另外
// 兩個「低風險」參數共用同一個RequireSession路由，但調高這個值會讓「之後所有新
// 建立的訂單」更容易把短付判定成COMPLETED——攻擊者只要偷到session cookie（不需要
// 偷到2FA裝置）就能讓商戶誤以為足額收款，符合CLAUDE.md「誤導商戶財務判斷」的
// RequireFreshTOTP判準，因此拆成獨立路由疊加step-up驗證，其餘兩欄維持原本的
// RequireSession不變。
func updateToleranceHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectParamsError(w, r, "bad_request")
			return
		}

		tolerancePercent, err := strconv.ParseFloat(strings.TrimSpace(r.PostFormValue("amount_tolerance_percent")), 64)
		if err != nil || tolerancePercent < 0 {
			redirectParamsError(w, r, "invalid_tolerance_percent")
			return
		}

		ctx := r.Context()
		_, err = deps.Pool.Exec(ctx, `
			UPDATE system_params SET amount_tolerance_percent = $1, updated_at = now() WHERE id = 1
		`, tolerancePercent)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		_ = audit.Log(ctx, deps.Pool, "admin", "AMOUNT_TOLERANCE_CHANGED", nil, nil, map[string]any{
			"amount_tolerance_percent": tolerancePercent,
		})

		http.Redirect(w, r, "/admin/params?success="+url.QueryEscape(successParamsUpdated), http.StatusSeeOther)
	}
}

const successParamsUpdated = "參數已更新，僅影響之後新建立的訂單"

func redirectParamsError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin/params?error="+code, http.StatusSeeOther)
}
