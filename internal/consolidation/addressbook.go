package consolidation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// addressBookMaxEntries is 需求書5.9's cap.
const addressBookMaxEntries = 30

// AddressBookEntry mirrors a consolidation_address_book row.
type AddressBookEntry struct {
	ID        int64
	Address   string
	Label     string
	CreatedAt time.Time
}

var (
	errInvalidAddressFormat     = errors.New("consolidation: invalid Tron address format")
	errAddressAlreadyExists     = errors.New("consolidation: address already in address book")
	errAddressBookFull          = errors.New("consolidation: address book is at its 30-entry limit")
	errAddressBookEntryNotFound = errors.New("consolidation: address book entry not found")
)

// ListAddressBook returns every entry, oldest first.
func ListAddressBook(ctx context.Context, deps Deps) ([]AddressBookEntry, error) {
	rows, err := deps.Pool.Query(ctx, `SELECT id, address, label, created_at FROM consolidation_address_book ORDER BY id`)
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

// CreateAddressBookEntry implements 需求書5.9「歸集地址簿」新增: format
// validation, duplicate rejection, and the 30-entry cap.
func CreateAddressBookEntry(ctx context.Context, deps Deps, address, label string) (AddressBookEntry, error) {
	if !isValidTronAddress(address) {
		return AddressBookEntry{}, errInvalidAddressFormat
	}

	var count int
	if err := deps.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consolidation_address_book`).Scan(&count); err != nil {
		return AddressBookEntry{}, err
	}
	if count >= addressBookMaxEntries {
		return AddressBookEntry{}, errAddressBookFull
	}

	var entry AddressBookEntry
	err := deps.Pool.QueryRow(ctx, `
		INSERT INTO consolidation_address_book (address, label) VALUES ($1, $2)
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

// RenameAddressBookEntry changes only the label — the address value itself
// is immutable once created (技術架構設計第10節).
func RenameAddressBookEntry(ctx context.Context, deps Deps, id int64, newLabel string) error {
	tag, err := deps.Pool.Exec(ctx, `UPDATE consolidation_address_book SET label = $2 WHERE id = $1`, id, newLabel)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAddressBookEntryNotFound
	}
	return nil
}

func DeleteAddressBookEntry(ctx context.Context, deps Deps, id int64) error {
	tag, err := deps.Pool.Exec(ctx, `DELETE FROM consolidation_address_book WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAddressBookEntryNotFound
	}
	return nil
}

// AutoSaveIfNew implements 技術架構設計第10節「歸集成功後，若該次目的地地址是手動輸入
// 的新地址...自動存入」: silently does nothing (no error) if address is
// already saved or the book is at its cap (需求書5.9: 達上限後新增一律跳過但不
// 影響歸集操作本身) — this runs as a side effect of an already-successful
// broadcast, so it must never fail or block that outcome.
func AutoSaveIfNew(ctx context.Context, deps Deps, address string) error {
	var exists bool
	if err := deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM consolidation_address_book WHERE address = $1)`, address).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	var count int
	if err := deps.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consolidation_address_book`).Scan(&count); err != nil {
		return err
	}
	if count >= addressBookMaxEntries {
		return nil
	}

	label := fmt.Sprintf("%s 新增（...%s）", time.Now().Format("2006-01-02"), lastNChars(address, 4))
	_, err := deps.Pool.Exec(ctx, `
		INSERT INTO consolidation_address_book (address, label) VALUES ($1, $2)
		ON CONFLICT (address) DO NOTHING
	`, address, label)
	return err
}

func lastNChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
