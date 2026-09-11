package checkout

import (
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

// statusView is the data the polled status fragment (status_region) renders.
// RemainingSeconds is a server-computed *relative* value re-sent on every
// response (initial page + each poll) so the browser never computes from an
// absolute timestamp of its own — avoids client clock drift (見第6節).
type statusView struct {
	Status           string
	Label            string
	Detail           string
	RemainingSeconds int64
	ShowCountdown    bool
	IsTerminal       bool
	StatusURL        string
}

// pageView embeds statusView so the full page template can pass itself to
// {{template "status_region" .}} and have the promoted fields resolve.
type pageView struct {
	statusView
	Address      string
	TargetAmount int64
	QRDataURI    template.URL
}

// pageHandler serves GET /checkout/{token}: the full payment page. Any
// unknown/malformed token is a 404 with no distinction between "malformed"
// and "not found" — the public_token is this page's only access control, so
// we must not enable probing (見第6節).
func pageHandler(pool *store.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		o, err := order.GetByPublicToken(r.Context(), pool, token)
		if err != nil {
			writeLookupError(w, err)
			return
		}

		vm := pageView{
			statusView:   buildStatusView(o, token),
			Address:      o.Address,
			TargetAmount: o.TargetAmount,
			QRDataURI:    qrDataURI(o.Address),
		}
		setCheckoutHeaders(w)
		render(w, http.StatusOK, "checkout_page.html", vm)
	}
}

// statusHandler serves GET /checkout/{token}/status: only the status region
// fragment, for the 5-second poll in checkout.js. The QR/address/amount never
// change, so they are not re-sent here.
func statusHandler(pool *store.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		o, err := order.GetByPublicToken(r.Context(), pool, token)
		if err != nil {
			writeLookupError(w, err)
			return
		}
		setCheckoutHeaders(w)
		render(w, http.StatusOK, "status_region", buildStatusView(o, token))
	}
}

// buildStatusView maps an order's status to the checkout display, applying
// the per-status branching from 技術架構設計第6節: only PENDING shows a
// countdown; CONFIRMING shows a no-time-pressure "confirming" message;
// terminal states stop polling. CONFIRMATION_STALLED and EXPIRED get
// deliberately distinct wording (第6節: 不可共用文案).
func buildStatusView(o order.Order, token string) statusView {
	v := statusView{
		Status:    string(o.Status),
		StatusURL: "/checkout/" + token + "/status",
	}
	switch o.Status {
	case order.StatusPending:
		v.Label = "等待付款"
		v.Detail = "請於剩餘時間內，將指定金額的 USDT 轉入下方收款地址。"
		v.ShowCountdown = true
		v.RemainingSeconds = remainingSeconds(o.ExpiresAt)
	case order.StatusConfirming:
		v.Label = "確認中"
		v.Detail = "已偵測到款項，正在等待區塊鏈確認，請耐心等候，勿重複付款。"
	case order.StatusCompleted:
		v.Label = "付款完成"
		v.Detail = "已收到並確認款項，付款完成，感謝您。"
		v.IsTerminal = true
	case order.StatusOverpaid:
		v.Label = "已收到超額款項"
		v.Detail = "已收到超過應付金額的款項，付款已完成；如需處理差額，請聯繫商戶。"
		v.IsTerminal = true
	case order.StatusConfirmationStalled:
		v.Label = "款項確認逾時"
		v.Detail = "已偵測到款項，但在確認時限內未完成鏈上確認，請聯繫商戶協助查證。"
		v.IsTerminal = true
	case order.StatusExpired:
		v.Label = "訂單已逾期"
		v.Detail = "有效期限內未收到足額款項，此訂單已失效，請聯繫商戶重新建立訂單。"
		v.IsTerminal = true
	}
	return v
}

// remainingSeconds returns whole seconds until t, clamped at 0. Recomputed
// on every response so a page opened right at expiry shows 0 rather than a
// stale positive value (the ~5s window where status may not yet have flipped
// to EXPIRED is an accepted system timing characteristic, 見第6節).
func remainingSeconds(t time.Time) int64 {
	secs := int64(time.Until(t).Seconds())
	if secs < 0 {
		return 0
	}
	return secs
}

// qrDataURI encodes address (the payload — only the raw address, no amount,
// since Tron has no widely-adopted payment-URI standard, 見第6節) as a PNG
// and returns it as a base64 data URI. Returned as template.URL so
// html/template does not rewrite the "data:" scheme to #ZgotmplZ. On encode
// failure returns empty (the template guards with {{if}}), so a QR problem
// never 500s the whole page.
func qrDataURI(address string) template.URL {
	png, err := qrcode.Encode(address, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)) //nolint:gosec // G203: value is a self-generated base64 PNG of a server-side address, not user HTML
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "not found", http.StatusNotFound)
}

// writeLookupError maps an order lookup error to an HTTP response: a missing
// order is a 404 with no "malformed vs not found" distinction (the
// public_token is the only access control — no probing, 見第6節); any other
// error (e.g. DB unavailable) is a 500. A DB outage returns 500 for every
// token alike, so it doesn't leak whether a given token exists.
func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, order.ErrOrderNotFound) {
		notFound(w)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// setCheckoutHeaders applies the baseline (non-strict) security headers from
// 第6節「安全性」: a CSP allowing self scripts (checkout.js) + inline styles +
// data: images (the QR), no framing; nosniff; and no-referrer so the
// high-entropy token URL isn't leaked via Referer.
func setCheckoutHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
