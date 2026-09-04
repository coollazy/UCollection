package webhook

import (
	"context"
	"testing"
)

func TestPendingSummary(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	orderID := testOrderID(t, pool)

	insertDelivery(t, pool, orderID, "pending", nil)
	insertDelivery(t, pool, orderID, "sending", nil)
	insertDelivery(t, pool, orderID, "failed", nil)
	insertDelivery(t, pool, orderID, "delivered", nil)
	insertDelivery(t, pool, orderID, "awaiting_config", nil)
	insertDelivery(t, pool, orderID, "awaiting_config", nil)

	failing, awaitingConfig, err := PendingSummary(context.Background(), pool)
	if err != nil {
		t.Fatalf("PendingSummary() error = %v", err)
	}
	if failing != 3 {
		t.Errorf("failing = %d, want 3 (pending+sending+failed)", failing)
	}
	if awaitingConfig != 2 {
		t.Errorf("awaitingConfig = %d, want 2", awaitingConfig)
	}
}
