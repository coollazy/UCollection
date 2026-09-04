// Package webhook delivers order state notifications with retry. See
// docs/開發流程框架-03-技術架構設計.md 第8節.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	httpTimeout          = 10 * time.Second
	userAgent            = "UCollection-Webhook/1"
	maxResponseBodyBytes = 4096 // response_body is stored truncated (技術架構設計第8節)
)

// newHTTPClient builds the client used for both automatic delivery and
// manual resend. It does not follow redirects — a 3xx is returned as-is so
// callers see it as a non-2xx (technical架構設計第8節「不follow重新導向（redirect
// 視為失敗）」).
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type sendResult struct {
	HTTPStatus   int
	ResponseBody string
}

// sendWebhook POSTs payload to url with the HMAC headers 技術架構設計第8節
// requires. secret is passed in by the caller, read fresh from
// system_params immediately before each call — never cached — so a secret
// rotation applies to the very next send attempt (第8節「webhook_secret
// 產生/測試/輪替」：輪替無過渡期).
func sendWebhook(ctx context.Context, client *http.Client, url, secret string, payload []byte) (sendResult, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return sendResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Signature", signature)

	resp, err := client.Do(req)
	if err != nil {
		return sendResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	return sendResult{HTTPStatus: resp.StatusCode, ResponseBody: string(body)}, nil
}
