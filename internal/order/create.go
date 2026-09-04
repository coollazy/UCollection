package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coollazy/UCollection/internal/hdwallet"
	"github.com/coollazy/UCollection/internal/store"
)

// ErrDuplicateMerchantOrderNo is returned when merchant_order_no already
// exists (建單API冪等性判斷，見技術架構設計第2節/第7節；由呼叫方internal/api決定如何回應).
var ErrDuplicateMerchantOrderNo = errors.New("order: merchant_order_no already exists")

// CreateParams are the caller-resolved values needed to create an order.
// The caller (internal/api, once built) is responsible for resolving these
// from the incoming request plus system_params defaults — CreateOrder does
// not read system_params itself, keeping this package's DB dependencies
// limited to what it directly owns (master_wallets/orders/...).
type CreateParams struct {
	MerchantOrderNo string
	MasterWalletID  int64
	TargetAmount    int64
	ValiditySeconds int64
	// AmountTolerancePercent is a percentage, e.g. 1.5 means 1.5%. It is
	// only ever used to derive an integer basis-points multiplier before
	// touching any amount value — money itself never leaves int64 (見
	// CLAUDE.md 安全鐵律6).
	AmountTolerancePercent          float64
	ConfirmationStallTimeoutSeconds int64
}

// CreateOrder allocates a derivation index under params.MasterWalletID
// (via pg_advisory_xact_lock, serializing concurrent allocations against
// the same wallet), derives its address, computes the amount tolerance
// bounds, and inserts the order row — all within one transaction.
func CreateOrder(ctx context.Context, pool *store.Pool, params CreateParams) (Order, error) {
	if params.TargetAmount <= 0 {
		return Order{}, fmt.Errorf("order: target amount must be positive, got %d", params.TargetAmount)
	}
	if params.AmountTolerancePercent < 0 {
		return Order{}, fmt.Errorf("order: amount tolerance percent must not be negative, got %v", params.AmountTolerancePercent)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("order: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, params.MasterWalletID); err != nil {
		return Order{}, fmt.Errorf("order: acquire master wallet lock: %w", err)
	}

	var xpub string
	var lastDerivedIndex int64
	err = tx.QueryRow(ctx, `SELECT xpub, last_derived_index FROM master_wallets WHERE id = $1`, params.MasterWalletID).
		Scan(&xpub, &lastDerivedIndex)
	if err != nil {
		return Order{}, fmt.Errorf("order: load master wallet %d: %w", params.MasterWalletID, err)
	}

	// index 0 永遠保留，不分配給任何訂單（CLAUDE.md 安全鐵律5）：分配一律先 +1 再用，
	// 第一筆訂單拿到的是 index 1。
	nextIndex := lastDerivedIndex + 1
	// Explicit bounds check before the int64->uint32 narrowing below: a
	// silent wraparound here would allocate an index that was already used
	// by an earlier order, breaking the "address<->order永久一對一綁定" invariant.
	// math.MaxInt32 (not MaxUint32) because BIP32 non-hardened indices only
	// use the low 31 bits — hdwallet.DeriveAddress rejects the high bit
	// being set, but that check happens after this cast, so it can't catch
	// wraparound on its own.
	if nextIndex > math.MaxInt32 {
		return Order{}, fmt.Errorf("order: derivation index %d exceeds non-hardened BIP32 range", nextIndex)
	}
	address, err := hdwallet.DeriveAddress(xpub, uint32(nextIndex)) //nolint:gosec // bounds-checked above
	if err != nil {
		return Order{}, fmt.Errorf("order: derive address for index %d: %w", nextIndex, err)
	}

	if _, err := tx.Exec(ctx, `UPDATE master_wallets SET last_derived_index = $1 WHERE id = $2`, nextIndex, params.MasterWalletID); err != nil {
		return Order{}, fmt.Errorf("order: advance last_derived_index: %w", err)
	}

	publicToken, err := randomToken()
	if err != nil {
		return Order{}, fmt.Errorf("order: generate public token: %w", err)
	}

	lower, upper := toleranceBounds(params.TargetAmount, params.AmountTolerancePercent)
	expiresAt := time.Now().Add(time.Duration(params.ValiditySeconds) * time.Second)

	row := tx.QueryRow(ctx, `
		INSERT INTO orders (
			merchant_order_no, public_token, master_wallet_id, derivation_index, address,
			target_amount, amount_lower_bound, amount_upper_bound,
			validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds,
			expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+orderColumns,
		params.MerchantOrderNo, publicToken, params.MasterWalletID, nextIndex, address,
		params.TargetAmount, lower, upper,
		params.ValiditySeconds, params.AmountTolerancePercent, params.ConfirmationStallTimeoutSeconds,
		expiresAt,
	)
	o, err := scanOrder(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "orders_merchant_order_no_key" {
			return Order{}, ErrDuplicateMerchantOrderNo
		}
		return Order{}, fmt.Errorf("order: insert order: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("order: commit: %w", err)
	}

	return o, nil
}

// toleranceBounds computes [lower, upper] around targetAmount for a given
// tolerance percent, entirely in int64 basis-points arithmetic after a
// single float->int64 rounding step on the percent value itself (a config
// number, not money) — see CreateParams.AmountTolerancePercent doc comment.
func toleranceBounds(targetAmount int64, tolerancePercent float64) (lower, upper int64) {
	toleranceBps := int64(math.Round(tolerancePercent * 100))
	tolerance := targetAmount * toleranceBps / 10000
	return targetAmount - tolerance, targetAmount + tolerance
}
