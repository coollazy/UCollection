package consolidation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// 手續費來源地址簿（需求書5.9 v0.39、技術架構設計第10節 v0.17）：與歸集地址簿
// （consolidation_address_book / addressbook.go）為兩份獨立表，結構相同、規則完全
// 比照，但分開儲存與管理。CRUD 邏輯刻意複製一份、不參數化共用表名（比照本專案「寧可
// 重複一份小 helper，也不跨用途硬共用」慣例）；error 值、格式驗證（isValidTronAddress）、
// 末碼工具（lastNChars）、上限常數（addressBookMaxEntries）皆沿用 addressbook.go
// 既有定義，不重複。回傳型別亦共用 AddressBookEntry（欄位相同）。

// ListFeeSourceBook returns every fee-source entry, oldest first.
func ListFeeSourceBook(ctx context.Context, deps Deps) ([]AddressBookEntry, error) {
	rows, err := deps.Pool.Query(ctx, `SELECT id, address, label, created_at FROM fee_source_address_book ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AddressBookEntry
	for rows.Next() {
		var e AddressBookEntry
		if err := rows.Scan(&e.ID, &e.Address, &e.Label, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateFeeSourceBookEntry mirrors CreateAddressBookEntry: format validation,
// duplicate rejection, and the 30-entry cap — see 需求書5.9「手續費來源地址簿」.
func CreateFeeSourceBookEntry(ctx context.Context, deps Deps, address, label string) (AddressBookEntry, error) {
	if !isValidTronAddress(address) {
		return AddressBookEntry{}, errInvalidAddressFormat
	}

	var count int
	if err := deps.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM fee_source_address_book`).Scan(&count); err != nil {
		return AddressBookEntry{}, err
	}
	if count >= addressBookMaxEntries {
		return AddressBookEntry{}, errAddressBookFull
	}

	var entry AddressBookEntry
	err := deps.Pool.QueryRow(ctx, `
		INSERT INTO fee_source_address_book (address, label) VALUES ($1, $2)
		RETURNING id, address, label, created_at
	`, address, label).Scan(&entry.ID, &entry.Address, &entry.Label, &entry.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return AddressBookEntry{}, errAddressAlreadyExists
		}
		return AddressBookEntry{}, err
	}
	return entry, nil
}

// RenameFeeSourceBookEntry changes only the label — the address value itself is
// immutable once created (技術架構設計第10節，規則比照歸集地址簿).
func RenameFeeSourceBookEntry(ctx context.Context, deps Deps, id int64, newLabel string) error {
	tag, err := deps.Pool.Exec(ctx, `UPDATE fee_source_address_book SET label = $2 WHERE id = $1`, id, newLabel)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAddressBookEntryNotFound
	}
	return nil
}

func DeleteFeeSourceBookEntry(ctx context.Context, deps Deps, id int64) error {
	tag, err := deps.Pool.Exec(ctx, `DELETE FROM fee_source_address_book WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAddressBookEntryNotFound
	}
	return nil
}

// AutoSaveFeeSourceIfNew is broadcastFeeTopupHandler's counterpart to
// AutoSaveIfNew (歸集地址簿): after a TRX top-up broadcast succeeds, save the
// manually-typed source address if it isn't already in the fee-source book.
// Runs as a best-effort side effect of an already-successful broadcast, so it
// must never fail or block that outcome — silently does nothing (no error) if
// the address is already saved or the book is at its cap (需求書5.9：達上限後
// 新增一律跳過但不影響操作本身).
func AutoSaveFeeSourceIfNew(ctx context.Context, deps Deps, address string) error {
	var exists bool
	if err := deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fee_source_address_book WHERE address = $1)`, address).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	var count int
	if err := deps.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM fee_source_address_book`).Scan(&count); err != nil {
		return err
	}
	if count >= addressBookMaxEntries {
		return nil
	}

	label := fmt.Sprintf("%s 新增（...%s）", time.Now().Format("2006-01-02"), lastNChars(address, 4))
	_, err := deps.Pool.Exec(ctx, `
		INSERT INTO fee_source_address_book (address, label) VALUES ($1, $2)
		ON CONFLICT (address) DO NOTHING
	`, address, label)
	return err
}
