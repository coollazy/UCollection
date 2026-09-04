// Package tronclient wraps calls to TronGrid, shared by internal/scanner
// and internal/admin (reverify). See
// docs/開發流程框架-03-技術架構設計.md 第1節、第4節、第11節.
package tronclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

const (
	apiKeyHeader = "TRON-PRO-API-KEY" //nolint:gosec // this is a header name, not a credential

	maxRetries     = 4
	baseRetryDelay = 500 * time.Millisecond
	maxRetryDelay  = 8 * time.Second
)

// Client is a thin HTTP wrapper around TronGrid's REST API. It retries
// transient failures (429, 5xx) with exponential backoff; it does not read
// configuration itself — callers (internal/scanner, internal/admin) pass in
// baseURL/apiKey sourced from internal/config, keeping this package
// dependency-free in that direction.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a Client. apiKey may be empty (TronGrid allows
// unauthenticated requests at a lower rate limit).
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doRequest executes a GET request against path (relative to baseURL,
// including any query string) and returns the raw response body. See
// doRequestWithRetry for retry semantics.
func (c *Client) doRequest(ctx context.Context, path string) ([]byte, error) {
	return c.doRequestWithRetry(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	}, path)
}

// doPostRequest executes a POST request with a JSON body against path and
// returns the raw response body. Used for TronGrid's read-only/side-effect-
// free `/wallet/*` endpoints (triggersmartcontract, createtransaction,
// gettransactioninfobyid) where retrying a failed attempt is safe. It is
// deliberately NOT used for broadcasttransaction — see BroadcastTransaction
// in transaction.go for why that one must never auto-retry.
func (c *Client) doPostRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.doRequestWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, path)
}

// doRequestWithRetry retries on HTTP 429 and 5xx responses (and network-
// level failures, which are treated as transient) with exponential backoff
// plus jitter, up to maxRetries attempts; other non-2xx statuses are
// returned immediately as errors without retrying, since retrying a client
// error (e.g. 400/404) cannot succeed. buildReq is called fresh on every
// attempt since an *http.Request's body reader can only be consumed once.
func (c *Client) doRequestWithRetry(ctx context.Context, buildReq func() (*http.Request, error), path string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		body, retryable, err := c.doRequestOnce(ctx, buildReq, path)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}

	return nil, fmt.Errorf("tronclient: exhausted %d retries: %w", maxRetries, lastErr)
}

func (c *Client) doRequestOnce(ctx context.Context, buildReq func() (*http.Request, error), path string) (body []byte, retryable bool, err error) {
	req, err := buildReq()
	if err != nil {
		return nil, false, fmt.Errorf("tronclient: build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network-level failures are transient by nature.
		return nil, true, fmt.Errorf("tronclient: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("tronclient: read response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		return respBody, false, nil
	}

	statusErr := fmt.Errorf("tronclient: %s returned status %d: %s", path, resp.StatusCode, truncate(respBody, 500))
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, statusErr
	}
	return nil, false, statusErr
}

// backoffDelay returns the delay before the given retry attempt (1-indexed),
// exponential with full jitter, capped at maxRetryDelay.
func backoffDelay(attempt int) time.Duration {
	d := baseRetryDelay << uint(attempt-1) //nolint:gosec // attempt is bounded by maxRetries
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	return time.Duration(rand.Int63n(int64(d))) //nolint:gosec // jitter, not security-sensitive
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
