package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coollazy/UCollection/internal/store"
)

// Log records one row in audit_logs (技術架構設計第2節/第9節). detail is
// marshaled to JSON; nil is stored as SQL NULL rather than the JSON literal
// "null" so callers with nothing to record can just pass nil.
func Log(ctx context.Context, pool *store.Pool, actor, actionType string, targetType *string, targetID *int64, detail map[string]any) error {
	var detailJSON []byte
	if detail != nil {
		var err error
		detailJSON, err = json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("audit: marshal detail: %w", err)
		}
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO audit_logs (actor, action_type, target_type, target_id, detail)
		VALUES ($1, $2, $3, $4, $5)
	`, actor, actionType, targetType, targetID, detailJSON)
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}
