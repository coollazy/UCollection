package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStaticCacheControl 覆蓋第16項：/static/ 回應必須帶 Cache-Control:no-cache，
// 讓瀏覽器每次重新驗證（避免啟發式快取導致更新後仍看到舊 JS），並確實把請求交給
// 內層 handler（FileServer）處理。
func TestStaticCacheControl(t *testing.T) {
	called := false
	h := staticCacheControl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/js/bundle.js", nil)
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}
