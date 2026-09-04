package order

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// manualWhitelist is 需求書5.2「手動改判」的完整白名單，逐字對應：
//   - CONFIRMATION_STALLED 可改判為 COMPLETED / OVERPAID / EXPIRED
//   - EXPIRED 可改判為 COMPLETED / OVERPAID
//
// This is the only place manual overrides are allowed to enter from — any
// other (from, to) pair is rejected before decide ever runs.
var manualWhitelist = map[Status]map[Status]bool{
	StatusConfirmationStalled: {
		StatusCompleted: true,
		StatusOverpaid:  true,
		StatusExpired:   true,
	},
	StatusExpired: {
		StatusCompleted: true,
		StatusOverpaid:  true,
	},
}

// ErrManualTransitionNotAllowed is returned when (from, to) is not in
// manualWhitelist.
var ErrManualTransitionNotAllowed = fmt.Errorf("order: manual transition not allowed")

// ManualTransition lets the merchant override a stuck order after manually
// re-checking the address on-chain (需求書5.2「手動改判」). Unlike the automatic
// path, it does not re-run any amount sub-query — the whole point is that
// the operator's manual verification overrides the system's own judgement
// (CLAUDE.md 業務鐵律5：這是設計，不是漏洞). It only enforces the status
// whitelist above.
func ManualTransition(ctx context.Context, pool *store.Pool, orderID int64, to Status, actor, note string) error {
	var allowedFrom []Status
	for from, targets := range manualWhitelist {
		if targets[to] {
			allowedFrom = append(allowedFrom, from)
		}
	}
	if len(allowedFrom) == 0 {
		return fmt.Errorf("%w: target status %s has no valid source", ErrManualTransitionNotAllowed, to)
	}

	return transition(ctx, pool, orderID, allowedFrom, func(_ context.Context, _ pgx.Tx, current Order) (Status, bool, map[string]any, error) {
		return to, true, map[string]any{"manual": true, "from": current.Status}, nil
	}, actor, note)
}
