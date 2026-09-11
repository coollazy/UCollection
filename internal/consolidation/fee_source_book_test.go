package consolidation

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// 這些測試比照 addressbook_test.go（目的地地址簿），驗證手續費來源地址簿為
// 一份行為完全對等、但獨立的清單。共用 testDestinationAddress / distinctTest
// Address 測試位址（addressbook_test.go / helpers_test.go 定義）。

func TestCreateFeeSourceBookEntry(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	entry, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "my label")
	if err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}
	if entry.Address != testDestinationAddress || entry.Label != "my label" {
		t.Errorf("entry = %+v", entry)
	}

	list, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil {
		t.Fatalf("ListFeeSourceBook() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}

func TestCreateFeeSourceBookEntry_RejectsInvalidFormat(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, "not-an-address", "label"); !errors.Is(err, errInvalidAddressFormat) {
		t.Fatalf("error = %v, want errInvalidAddressFormat", err)
	}
}

func TestCreateFeeSourceBookEntry_RejectsDuplicate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "first"); err != nil {
		t.Fatalf("first CreateFeeSourceBookEntry() error = %v", err)
	}
	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "second"); !errors.Is(err, errAddressAlreadyExists) {
		t.Fatalf("error = %v, want errAddressAlreadyExists", err)
	}
}

func TestCreateFeeSourceBookEntry_RejectsAtCap(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	for i := 0; i < addressBookMaxEntries; i++ {
		addr := distinctTestAddress(t, i)
		if _, err := CreateFeeSourceBookEntry(context.Background(), deps, addr, fmt.Sprintf("entry %d", i)); err != nil {
			t.Fatalf("CreateFeeSourceBookEntry() #%d error = %v", i, err)
		}
	}

	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "over the cap"); !errors.Is(err, errAddressBookFull) {
		t.Fatalf("error = %v, want errAddressBookFull", err)
	}
}

func TestRenameAndDeleteFeeSourceBookEntry(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	entry, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "old label")
	if err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}

	if err := RenameFeeSourceBookEntry(context.Background(), deps, entry.ID, "new label"); err != nil {
		t.Fatalf("RenameFeeSourceBookEntry() error = %v", err)
	}
	list, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Label != "new label" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}

	if err := DeleteFeeSourceBookEntry(context.Background(), deps, entry.ID); err != nil {
		t.Fatalf("DeleteFeeSourceBookEntry() error = %v", err)
	}
	list, err = ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(list) != 0 {
		t.Fatalf("list after delete = %+v, err = %v", list, err)
	}
}

func TestRenameFeeSourceBookEntry_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if err := RenameFeeSourceBookEntry(context.Background(), deps, 999999, "x"); !errors.Is(err, errAddressBookEntryNotFound) {
		t.Fatalf("error = %v, want errAddressBookEntryNotFound", err)
	}
}

func TestAutoSaveFeeSourceIfNew_SkipsExisting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if _, err := CreateFeeSourceBookEntry(context.Background(), deps, testDestinationAddress, "manual"); err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}
	if err := AutoSaveFeeSourceIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveFeeSourceIfNew() error = %v", err)
	}

	list, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Label != "manual" {
		t.Fatalf("list = %+v, err = %v — AutoSaveFeeSourceIfNew must not touch an existing entry's label", list, err)
	}
}

func TestAutoSaveFeeSourceIfNew_AddsNewAddress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	if err := AutoSaveFeeSourceIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveFeeSourceIfNew() error = %v", err)
	}
	list, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(list) != 1 || list[0].Address != testDestinationAddress {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
}

func TestAutoSaveFeeSourceIfNew_SilentlySkipsAtCap(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	for i := 0; i < addressBookMaxEntries; i++ {
		addr := distinctTestAddress(t, i)
		if _, err := CreateFeeSourceBookEntry(context.Background(), deps, addr, fmt.Sprintf("entry %d", i)); err != nil {
			t.Fatalf("CreateFeeSourceBookEntry() #%d error = %v", i, err)
		}
	}

	// Must return nil, not an error — a full book must never block or fail an
	// already-successful fee-topup broadcast.
	if err := AutoSaveFeeSourceIfNew(context.Background(), deps, testDestinationAddress); err != nil {
		t.Fatalf("AutoSaveFeeSourceIfNew() at cap error = %v, want nil", err)
	}
	list, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil || len(list) != addressBookMaxEntries {
		t.Fatalf("len(list) = %d, want unchanged %d", len(list), addressBookMaxEntries)
	}
}
