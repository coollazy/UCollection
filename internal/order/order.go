// Package order implements the order state machine. See
// docs/開發流程框架-03-技術架構設計.md 第5節.
package order

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"time"
)

// Status is one of the six order states defined in 需求書 5.2.
type Status string

const (
	StatusPending             Status = "PENDING"
	StatusConfirming          Status = "CONFIRMING"
	StatusCompleted           Status = "COMPLETED"
	StatusOverpaid            Status = "OVERPAID"
	StatusConfirmationStalled Status = "CONFIRMATION_STALLED"
	StatusExpired             Status = "EXPIRED"
)

// terminalStatuses are the statuses that trigger a webhook_deliveries INSERT
// on entry (第8節「觸發整合」).
var terminalStatuses = map[Status]bool{
	StatusCompleted:           true,
	StatusOverpaid:            true,
	StatusConfirmationStalled: true,
	StatusExpired:             true,
}

// orderColumns is the single source of truth for the column list used by
// every SELECT/RETURNING that populates an Order, so the list and scanOrder
// can never drift apart from each other.
const orderColumns = `
	id, merchant_order_no, public_token, master_wallet_id, derivation_index, address,
	status, target_amount, amount_lower_bound, amount_upper_bound,
	validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds,
	expires_at, confirming_at, consolidation_status, created_at, updated_at
`

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query).
type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrder(row rowScanner) (Order, error) {
	var o Order
	err := row.Scan(
		&o.ID, &o.MerchantOrderNo, &o.PublicToken, &o.MasterWalletID, &o.DerivationIndex, &o.Address,
		&o.Status, &o.TargetAmount, &o.AmountLowerBound, &o.AmountUpperBound,
		&o.ValiditySeconds, &o.AmountTolerancePercent, &o.ConfirmationStallTimeoutSeconds,
		&o.ExpiresAt, &o.ConfirmingAt, &o.ConsolidationStatus, &o.CreatedAt, &o.UpdatedAt,
	)
	return o, err
}

// randomToken generates a URL-safe token from 32 bytes of crypto/rand,
// used for both orders.public_token and webhook_deliveries.event_id (見
// 技術架構設計第2節/第8節「比照 public_token 慣例」).
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Order mirrors the orders table row.
type Order struct {
	ID                              int64
	MerchantOrderNo                 string
	PublicToken                     string
	MasterWalletID                  int64
	DerivationIndex                 int64
	Address                         string
	Status                          Status
	TargetAmount                    int64
	AmountLowerBound                int64
	AmountUpperBound                int64
	ValiditySeconds                 int64
	AmountTolerancePercent          float64
	ConfirmationStallTimeoutSeconds int64
	ExpiresAt                       time.Time
	ConfirmingAt                    *time.Time
	ConsolidationStatus             string
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}
