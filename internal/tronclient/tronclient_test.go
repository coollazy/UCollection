package tronclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoRequest_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"success":true,"meta":{}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	body, err := c.doRequest(context.Background(), "/ping")
	if err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
	if string(body) != `{"data":[],"success":true,"meta":{}}` {
		t.Errorf("doRequest() body = %q", body)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestDoRequest_RetriesExhaustedOn5xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if _, err := c.doRequest(context.Background(), "/ping"); err == nil {
		t.Fatal("doRequest() error = nil, want error after exhausting retries")
	}
	if got := attempts.Load(); got != maxRetries+1 {
		t.Errorf("attempts = %d, want %d (1 initial + %d retries)", got, maxRetries+1, maxRetries)
	}
}

func TestDoRequest_NoRetryOnClientError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if _, err := c.doRequest(context.Background(), "/ping"); err == nil {
		t.Fatal("doRequest() error = nil, want error for 404")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on non-retryable status)", got)
	}
}

func TestDoRequest_SendsAPIKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(apiKeyHeader); got != "test-key" {
			t.Errorf("%s header = %q, want %q", apiKeyHeader, got, "test-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	if _, err := c.doRequest(context.Background(), "/ping"); err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
}
