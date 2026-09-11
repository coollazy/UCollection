package consolidation

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 這些測試比照 handlers_addressbook_test.go，驗證手續費來源地址簿的 HTTP 層
// （頁面渲染、新增/改名/刪除、TOTP 保護）與目的地地址簿行為對等但獨立。

func TestFeeSourceBookPage_Renders(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, testSourceAddress, "existing fee source"); err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation/fee-source-book")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "existing fee source") || !strings.Contains(body, testSourceAddress) {
		t.Fatalf("body missing existing entry: %s", body)
	}
}

func TestFeeSourceBookSubmit_Create(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-source-book", url.Values{
		"id":        {""},
		"address":   {testSourceAddress},
		"label":     {"new fee source"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/consolidation/fee-source-book" {
		t.Fatalf("status=%d location=%q, want 303 to the fee source book page", resp.StatusCode, resp.Header.Get("Location"))
	}

	entries, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(entries) != 1 || entries[0].Label != "new fee source" {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}

	assertAuditCount(t, pool, "FEE_SOURCE_BOOK_ENTRY_CREATED", 1)
}

func TestFeeSourceBookSubmit_CreateInvalidFormatRedirectsWithError(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-source-book", url.Values{
		"id":        {""},
		"address":   {"not-an-address"},
		"label":     {"whatever"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Query().Get("error") != "invalid_format" {
		t.Fatalf("Location = %q, want error=invalid_format", resp.Header.Get("Location"))
	}
}

func TestFeeSourceBookSubmit_Rename(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	entry, err := CreateFeeSourceBookEntry(context.Background(), deps, testSourceAddress, "old label")
	if err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-source-book", url.Values{
		"id":        {strconv.FormatInt(entry.ID, 10)},
		"address":   {testSourceAddress},
		"label":     {"renamed"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(entries) != 1 || entries[0].Label != "renamed" {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}

	assertAuditCount(t, pool, "FEE_SOURCE_BOOK_ENTRY_RENAMED", 1)
}

func TestFeeSourceBookDelete_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	entry, err := CreateFeeSourceBookEntry(context.Background(), deps, testSourceAddress, "to delete")
	if err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := doDeleteWithTOTPCode(t, srv, cookie, "/admin/consolidation/fee-source-book/"+strconv.FormatInt(entry.ID, 10), totpCodeAt(t, secret, time.Now()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v, err = %v, want empty after delete", entries, err)
	}

	assertAuditCount(t, pool, "FEE_SOURCE_BOOK_ENTRY_DELETED", 1)
}

// TestFeeSourceBookWriteRoutes_RequireFreshTOTP mirrors the address book's
// same test — POST/DELETE without a totp_code redirect to returnTo with
// totp_error=missing (ADR-0016, no freshness grace window).
func TestFeeSourceBookWriteRoutes_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	postResp := postForm(t, srv, cookie, "/admin/consolidation/fee-source-book", url.Values{"id": {""}, "address": {testSourceAddress}, "label": {"x"}})
	postLoc, err := url.Parse(postResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if postResp.StatusCode != http.StatusSeeOther || postLoc.Path != "/admin/consolidation/fee-source-book" || postLoc.Query().Get("totp_error") != "missing" {
		t.Fatalf("POST: status=%d location=%q, want 303 to /admin/consolidation/fee-source-book?totp_error=missing", postResp.StatusCode, postResp.Header.Get("Location"))
	}

	deleteResp := doDelete(t, srv, cookie, "/admin/consolidation/fee-source-book/1")
	deleteLoc, err := url.Parse(deleteResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if deleteResp.StatusCode != http.StatusSeeOther || deleteLoc.Path != "/admin/consolidation/fee-source-book" || deleteLoc.Query().Get("totp_error") != "missing" {
		t.Fatalf("DELETE: status=%d location=%q, want 303 to /admin/consolidation/fee-source-book?totp_error=missing", deleteResp.StatusCode, deleteResp.Header.Get("Location"))
	}
}
