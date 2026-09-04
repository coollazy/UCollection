package scanner

import (
	"context"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// ReverifyOrder is internal/admin's "手動重新檢查此地址" (技術架構設計第11節).
// It reuses the exact same contract-events endpoint and pagination rules as
// 任務2/3 (not an account-level endpoint — see 第11節 for why), scoped to one
// order's address and to [order.CreatedAt, now()]. Unlike 任務2/3 it does
// NOT evaluate any automatic state transition afterward — 「本操作本身不觸發任何
// 自動狀態轉換...商戶查看更新後的數據，若判斷需要變更狀態，另行呼叫...手動改判端點」
// (第11節).
func ReverifyOrder(ctx context.Context, deps Deps, ord order.Order) error {
	minTs := ord.CreatedAt.UnixMilli()

	// only_confirmed=false pass — merge every detected transfer, mirroring
	// 任務2 (task_events.go) minus the EvaluateConfirming call.
	if err := reverifyPass(ctx, deps, ord.Address, minTs, false); err != nil {
		return err
	}
	// only_confirmed=true pass — mark the finalized subset, mirroring
	// 任務3 (task_finality.go) minus the EvaluateFinal call.
	return reverifyPass(ctx, deps, ord.Address, minTs, true)
}

// reverifyPass pages through ContractEvents exactly like scanEventsOnce/
// scanFinalityOnce do (max_block_timestamp intentionally omitted — that's
// the default "up to now" behaviour those two tasks already rely on, so an
// explicit now() adds nothing), but scoped to one address and without
// touching scan_checkpoint or evaluating any transition.
func reverifyPass(ctx context.Context, deps Deps, address string, minTs int64, onlyConfirmed bool) error {
	fingerprint := ""
	for {
		page, err := deps.TronClient.ContractEvents(ctx, deps.ContractAddress, tronclient.EventsQuery{
			EventName:         contractEventName,
			OnlyConfirmed:     onlyConfirmed,
			MinBlockTimestamp: minTs,
			Fingerprint:       fingerprint,
			Limit:             pageLimit,
		})
		if err != nil {
			return err
		}

		for _, ev := range page.Events {
			if ev.To != address {
				continue
			}
			if onlyConfirmed {
				if _, _, err := markEventConfirmed(ctx, deps.Pool, ev); err != nil {
					return err
				}
			} else {
				if _, _, err := mergeIncomingTransaction(ctx, deps.Pool, ev); err != nil {
					return err
				}
			}
		}

		if page.NextFingerprint == "" {
			break
		}
		fingerprint = page.NextFingerprint
	}
	return nil
}
