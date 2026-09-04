package consolidation

import (
	"context"
	"errors"
)

var errInvalidFeeSource = errors.New("consolidation: fee_source must be A1 or A2")

// CreateConsolidationBatch opens a new USDT consolidation batch (one
// destination address, potentially many source orders — 技術架構設計第10節).
// Item rows are created later, one per order, at prepare time (see
// prepare.go).
func CreateConsolidationBatch(ctx context.Context, deps Deps, masterWalletID int64, destinationAddress string) (int64, error) {
	if !isValidTronAddress(destinationAddress) {
		return 0, errInvalidAddressFormat
	}
	var id int64
	err := deps.Pool.QueryRow(ctx, `
		INSERT INTO consolidation_batches (master_wallet_id, destination_address) VALUES ($1, $2)
		RETURNING id
	`, masterWalletID, destinationAddress).Scan(&id)
	return id, err
}

// CreateFeeTopupBatch opens a new TRX fee-topup batch. A1/A2 are
// technically identical mechanisms (技術架構設計第10節「A1/A2採完全相同的技術機
// 制」) — fee_source is purely an audit-trail classification the operator
// picks.
func CreateFeeTopupBatch(ctx context.Context, deps Deps, masterWalletID int64, feeSource, feeSourceAddress string) (int64, error) {
	if feeSource != "A1" && feeSource != "A2" {
		return 0, errInvalidFeeSource
	}
	if !isValidTronAddress(feeSourceAddress) {
		return 0, errInvalidAddressFormat
	}
	var id int64
	err := deps.Pool.QueryRow(ctx, `
		INSERT INTO fee_topup_batches (master_wallet_id, fee_source, fee_source_address) VALUES ($1, $2, $3)
		RETURNING id
	`, masterWalletID, feeSource, feeSourceAddress).Scan(&id)
	return id, err
}
