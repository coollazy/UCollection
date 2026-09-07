package consolidation

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func doGet(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func doDelete(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path string) *http.Response {
	t.Helper()
	return doDeleteWithTOTPCode(t, srv, cookie, path, "")
}

// doDeleteWithTOTPCode sends the code the same way admin.js's fetch()-based
// DELETE does (X-Totp-Code header, ADR-0016's totpCodeFromRequest checks
// that before falling back to a form field a DELETE request has none of).
func doDeleteWithTOTPCode(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path, code string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if code != "" {
		req.Header.Set("X-Totp-Code", code)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func postForm(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
