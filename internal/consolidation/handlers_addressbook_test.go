package consolidation

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/store"
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
	cookie := newActiveSessionCookie(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":      {""},
		"address": {testDestinationAddress},
		"label":   {"new entry"},
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
	cookie := newActiveSessionCookie(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":      {""},
		"address": {"not-an-address"},
		"label":   {"whatever"},
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
	cookie := newActiveSessionCookie(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":      {strconv.FormatInt(entry.ID, 10)},
		"address": {testDestinationAddress},
		"label":   {"renamed"},
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
	cookie := newActiveSessionCookie(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{
		"id":      {"999999"},
		"address": {testDestinationAddress},
		"label":   {"x"},
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
	cookie := newActiveSessionCookie(t, pool)

	resp := doDelete(t, srv, cookie, "/admin/consolidation/address-book/"+strconv.FormatInt(entry.ID, 10))
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
	cookie := newActiveSessionCookie(t, pool)

	resp := doDelete(t, srv, cookie, "/admin/consolidation/address-book/999999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAddressBookWriteRoutes_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	srv := newTestMux(t, deps)

	// An active session whose TOTP verification is stale (>15 minutes) must
	// be redirected to reverify before either write route runs, same as
	// /tron-proxy/... in Part 1.
	cookie := newStaleActiveSessionCookie(t, pool)

	postResp := postForm(t, srv, cookie, "/admin/consolidation/address-book", url.Values{"id": {""}, "address": {testDestinationAddress}, "label": {"x"}})
	if postResp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(postResp.Header.Get("Location"), "/admin/reverify-totp") {
		t.Fatalf("POST: status=%d location=%q, want 303 to reverify", postResp.StatusCode, postResp.Header.Get("Location"))
	}

	deleteResp := doDelete(t, srv, cookie, "/admin/consolidation/address-book/1")
	if deleteResp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(deleteResp.Header.Get("Location"), "/admin/reverify-totp") {
		t.Fatalf("DELETE: status=%d location=%q, want 303 to reverify", deleteResp.StatusCode, deleteResp.Header.Get("Location"))
	}
}

func newStaleActiveSessionCookie(t *testing.T, pool *store.Pool) *http.Cookie {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	_, err := pool.Exec(context.Background(), `
		INSERT INTO admin_sessions (token_hash, status, expires_at, last_seen_at, last_totp_verified_at)
		VALUES ($1, 'active', now() + interval '1 hour', now(), now() - interval '16 minutes')
	`, tokenHash)
	if err != nil {
		t.Fatalf("insert admin_sessions: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token} //nolint:gosec // test-only outgoing request cookie, not a real Set-Cookie response
}
