package consolidation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/coollazy/UCollection/internal/hdwallet"
)

func TestCreateAddressBookEntry(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	entry, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "my label")
	if err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	if entry.Address != testDestinationAddress || entry.Label != "my label" {
		t.Errorf("entry = %+v", entry)
	}

	list, err := ListAddressBook(context.Background(), deps)
	if err != nil {
		t.Fatalf("ListAddressBook() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}

func TestCreateAddressBookEntry_RejectsInvalidFormat(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateAddressBookEntry(context.Background(), deps, "not-an-address", "label"); !errors.Is(err, errInvalidAddressFormat) {
		t.Fatalf("error = %v, want errInvalidAddressFormat", err)
	}
}

func TestCreateAddressBookEntry_RejectsDuplicate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "first"); err != nil {
		t.Fatalf("first CreateAddressBookEntry() error = %v", err)
	}
	if _, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "second"); !errors.Is(err, errAddressAlreadyExists) {
		t.Fatalf("error = %v, want errAddressAlreadyExists", err)
	}
}

func TestCreateAddressBookEntry_RejectsAtCap(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	for i := 0; i < addressBookMaxEntries; i++ {
		addr := distinctTestAddress(t, i)
		if _, err := CreateAddressBookEntry(context.Background(), deps, addr, fmt.Sprintf("entry %d", i)); err != nil {
			t.Fatalf("CreateAddressBookEntry() #%d error = %v", i, err)
		}
	}

	if _, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "over the cap"); !errors.Is(err, errAddressBookFull) {
		t.Fatalf("error = %v, want errAddressBookFull", err)
	}
}

func TestRenameAndDeleteAddressBookEntry(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	entry, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "old label")
	if err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}

	if err := RenameAddressBookEntry(context.Background(), deps, entry.ID, "new label"); err != nil {
		t.Fatalf("RenameAddressBookEntry() error = %v", err)
	}
	list, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Label != "new label" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}

	if err := DeleteAddressBookEntry(context.Background(), deps, entry.ID); err != nil {
		t.Fatalf("DeleteAddressBookEntry() error = %v", err)
	}
	list, err = ListAddressBook(context.Background(), deps)
	if err != nil || len(list) != 0 {
		t.Fatalf("list after delete = %+v, err = %v", list, err)
	}
}

func TestRenameAddressBookEntry_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if err := RenameAddressBookEntry(context.Background(), deps, 999999, "x"); !errors.Is(err, errAddressBookEntryNotFound) {
		t.Fatalf("error = %v, want errAddressBookEntryNotFound", err)
	}
}

func TestAutoSaveIfNew_SkipsExisting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateAddressBookEntry(context.Background(), deps, testDestinationAddress, "manual"); err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	if err := AutoSaveIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveIfNew() error = %v", err)
	}

	list, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Label != "manual" {
		t.Fatalf("list = %+v, err = %v — AutoSaveIfNew must not touch an existing entry's label", list, err)
	}
}

func TestAutoSaveIfNew_AddsNewAddress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if err := AutoSaveIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveIfNew() error = %v", err)
	}
	list, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Address != testDestinationAddress {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
}

func TestAutoSaveIfNew_SilentlySkipsAtCap(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	for i := 0; i < addressBookMaxEntries; i++ {
		addr := distinctTestAddress(t, i)
		if _, err := CreateAddressBookEntry(context.Background(), deps, addr, fmt.Sprintf("entry %d", i)); err != nil {
			t.Fatalf("CreateAddressBookEntry() #%d error = %v", i, err)
		}
	}

	// Must return nil, not an error — a full address book must never block
	// or fail an already-successful consolidation broadcast.
	if err := AutoSaveIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveIfNew() at cap error = %v, want nil", err)
	}
	list, err := ListAddressBook(context.Background(), deps)
	if err != nil || len(list) != addressBookMaxEntries {
		t.Fatalf("len(list) = %d, want unchanged %d", len(list), addressBookMaxEntries)
	}
}

// distinctTestAddress derives N distinct, validly-formatted Tron addresses
// by deriving from testXpub at different indexes — reuses
// internal/hdwallet's already-cross-validated derivation rather than
// hand-crafting fake Base58Check strings.
func distinctTestAddress(t *testing.T, index int) string {
	t.Helper()
	addr, err := hdwallet.DeriveAddress(testXpub, uint32(index)+1) //nolint:gosec // index is a small test loop counter, well within uint32 range
	if err != nil {
		t.Fatalf("hdwallet.DeriveAddress(%d): %v", index, err)
	}
	return addr
}
