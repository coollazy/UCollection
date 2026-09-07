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

func TestAddressBookPage_Renders(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	if _, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "existing label"); err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation/address-book")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "existing label") || !strings.Contains(body, testDestinationAddress) {
		t.Fatalf("body missing existing entry: %s", body)
	}
}

func TestAddressBookSubmit_Create(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":        {""},
		"address":   {testDestinationAddress},
		"label":     {"new entry"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/consolidation/address-book" {
		t.Fatalf("status=%d location=%q, want 303 to the address book page", resp.StatusCode, resp.Header.Get("Location"))
	}

	entries, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(entries) != 1 || entries[0].Label != "new entry" {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}

	assertAuditCount(t, pool, "ADDRESS_BOOK_ENTRY_CREATED", 1)
}

func TestAddressBookSubmit_CreateInvalidFormatRedirectsWithError(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
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

func TestAddressBookSubmit_Rename(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	entry, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "old label")
	if err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":        {strconv.FormatInt(entry.ID, 10)},
		"address":   {testDestinationAddress},
		"label":     {"renamed"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(entries) != 1 || entries[0].Label != "renamed" {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}

	assertAuditCount(t, pool, "ADDRESS_BOOK_ENTRY_RENAMED", 1)
}

func TestAddressBookSubmit_RenameNotFoundRedirectsWithError(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":        {"999999"},
		"address":   {testDestinationAddress},
		"label":     {"x"},
		"totp_code": {totpCodeAt(t, secret, time.Now())},
	})
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Query().Get("error") != "not_found" {
		t.Fatalf("Location = %q, want error=not_found", resp.Header.Get("Location"))
	}
}

func TestAddressBookDelete_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	entry, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "to delete")
	if err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := doDeleteWithTOTPCode(t, srv, cookie, "/admin/consolidation/address-book/"+strconv.FormatInt(entry.ID, 10), totpCodeAt(t, secret, time.Now()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v, err = %v, want empty after delete", entries, err)
	}

	assertAuditCount(t, pool, "ADDRESS_BOOK_ENTRY_DELETED", 1)
}

func TestAddressBookDelete_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := doDeleteWithTOTPCode(t, srv, cookie, "/admin/consolidation/address-book/999999", totpCodeAt(t, secret, time.Now()))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestAddressBookWriteRoutes_RequireFreshTOTP ADR-0016: every POST/DELETE
// requires its own totp_code every time (no freshness grace window) — a
// request with no code redirects to returnTo with totp_error=missing, not
// to the separate /admin/reverify-totp page (that's only for GET-registered
// routes, which have no body to carry a code in).
func TestAddressBookWriteRoutes_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	postResp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{"id": {""}, "address": {testDestinationAddress}, "label": {"x"}})
	postLoc, err := url.Parse(postResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if postResp.StatusCode != http.StatusSeeOther || postLoc.Path != "/admin/consolidation/address-book" || postLoc.Query().Get("totp_error") != "missing" {
		t.Fatalf("POST: status=%d location=%q, want 303 to /admin/consolidation/address-book?totp_error=missing", postResp.StatusCode, postResp.Header.Get("Location"))
	}

	deleteResp := doDelete(t, srv, cookie, "/admin/consolidation/address-book/1")
	deleteLoc, err := url.Parse(deleteResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if deleteResp.StatusCode != http.StatusSeeOther || deleteLoc.Path != "/admin/consolidation/address-book" || deleteLoc.Query().Get("totp_error") != "missing" {
		t.Fatalf("DELETE: status=%d location=%q, want 303 to /admin/consolidation/address-book?totp_error=missing", deleteResp.StatusCode, deleteResp.Header.Get("Location"))
	}
}
