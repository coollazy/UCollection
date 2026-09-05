package admin

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/webhook"
)

const maxTestResponseSnippet = 500

type testPingResult struct {
	HTTPStatus int
	Body       string
	Err        string
}

type webhookConfigPageData struct {
	URL              string
	SecretConfigured bool
	FlashError       string
	FlashSuccess     string
	TestResult       *testPingResult
	GeneratedSecret  string // set only right after a "generate" secret rotation, shown once
}

func webhookConfigErrorMessage(code string) string {
	switch code {
	case "invalid_url":
		return "Webhook URL 格式不正確，須為 http(s) 開頭的網址"
	case "invalid_secret":
		return "Webhook secret 不可為空白"
	case "bad_request":
		return "表單格式錯誤"
	default:
		return ""
	}
}

func loadWebhookConfigPage(w http.ResponseWriter, r *http.Request, deps Deps, extra webhookConfigPageData) {
	var u, s *string
	err := deps.Pool.QueryRow(r.Context(), `SELECT webhook_url, webhook_secret FROM system_params WHERE id = 1`).Scan(&u, &s)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := extra
	if u != nil {
		data.URL = *u
	}
	data.SecretConfigured = s != nil && *s != ""
	render(w, http.StatusOK, "webhook_config.html", data)
}

// webhookConfigPageHandler implements GET /admin/webhook-config (技術架構設計
// 第11節「Webhook 端點設定」：查看目前設定，webhook_secret不明碼顯示，只顯示是否已
// 設定).
func webhookConfigPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		loadWebhookConfigPage(w, r, deps, webhookConfigPageData{
			FlashError:   webhookConfigErrorMessage(q.Get("error")),
			FlashSuccess: q.Get("success"),
		})
	}
}

// updateWebhookURLHandler implements POST /admin/webhook-config (技術架構設計
// 第11節：風險是「URL被竄改重導向」而非URL外洩本身，故疊加RequireFreshTOTP). Empty
// value clears the setting.
func updateWebhookURLHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectWebhookConfigError(w, r, "bad_request")
			return
		}
		rawURL := strings.TrimSpace(r.PostFormValue("webhook_url"))
		if rawURL != "" {
			parsed, err := url.ParseRequestURI(rawURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				redirectWebhookConfigError(w, r, "invalid_url")
				return
			}
		}

		ctx := r.Context()
		if _, err := deps.Pool.Exec(ctx, `UPDATE system_params SET webhook_url = NULLIF($1, ''), updated_at = now() WHERE id = 1`, rawURL); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		_ = audit.Log(ctx, deps.Pool, "admin", "WEBHOOK_URL_CHANGED", nil, nil, map[string]any{"webhook_url": rawURL})

		http.Redirect(w, r, "/admin/webhook-config?success="+url.QueryEscape("Webhook URL 已更新"), http.StatusSeeOther)
	}
}

// updateWebhookSecretHandler implements POST /admin/webhook-config/secret
// (技術架構設計第11節：「產生（系統crypto/rand）或手動輸入webhook_secret」，輪替無過渡
// 期). mode=generate值operator不知道新secret，故不303 redirect，直接200顯示一次；
// mode=manual操作者已知道自己輸入的值，303 redirect即可.
func updateWebhookSecretHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectWebhookConfigError(w, r, "bad_request")
			return
		}
		mode := r.PostFormValue("mode")

		var secret string
		switch mode {
		case "generate":
			s, err := randomAPIKeyMaterial() // same crypto/rand + base64.RawURLEncoding shape
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			secret = s
		case "manual":
			secret = strings.TrimSpace(r.PostFormValue("secret"))
			if secret == "" {
				redirectWebhookConfigError(w, r, "invalid_secret")
				return
			}
		default:
			redirectWebhookConfigError(w, r, "bad_request")
			return
		}

		ctx := r.Context()
		if _, err := deps.Pool.Exec(ctx, `UPDATE system_params SET webhook_secret = $1, updated_at = now() WHERE id = 1`, secret); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		_ = audit.Log(ctx, deps.Pool, "admin", "WEBHOOK_SECRET_ROTATED", nil, nil, map[string]any{"mode": mode})

		if mode == "generate" {
			loadWebhookConfigPage(w, r, deps, webhookConfigPageData{GeneratedSecret: secret})
			return
		}
		http.Redirect(w, r, "/admin/webhook-config?success="+url.QueryEscape("Webhook secret 已更新，請同步更新驗簽端設定"), http.StatusSeeOther)
	}
}

// testWebhookHandler implements POST /admin/webhook-config/test (技術架構設計
// 第11節：同步打一次，不落地、不消耗重試額度，RequireSession即可).
func testWebhookHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, body, err := webhook.SendTestPing(r.Context(), deps.Pool, deps.HTTPClient)
		result := &testPingResult{HTTPStatus: status}
		if err != nil {
			if errors.Is(err, webhook.ErrWebhookNotConfigured) {
				result.Err = "尚未設定 webhook_url/webhook_secret"
			} else {
				result.Err = "傳送失敗：" + err.Error()
			}
		} else {
			snippet := body
			if len(snippet) > maxTestResponseSnippet {
				snippet = snippet[:maxTestResponseSnippet]
			}
			result.Body = snippet
		}
		loadWebhookConfigPage(w, r, deps, webhookConfigPageData{TestResult: result})
	}
}

func redirectWebhookConfigError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin/webhook-config?error="+code, http.StatusSeeOther)
}
