package webhook

import (
	"context"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
)

func TestListForOrder(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	orderID := testOrderID(t, pool)

	// A second order under the SAME master wallet as testOrderID's, so its
	// derivation index (and thus address) doesn't collide with orderID's —
	// two separate master_wallets rows sharing testXpub would both start
	// deriving from index 1 and hit orders.address's unique constraint.
	var walletID int64
	if err := pool.QueryRow(context.Background(), `SELECT master_wallet_id FROM orders WHERE id = $1`, orderID).Scan(&walletID); err != nil {
		t.Fatalf("load wallet id: %v", err)
	}
	other, err := order.CreateOrder(context.Background(), pool, order.CreateParams{
		MerchantOrderNo:                 randomHex(t),
		MasterWalletID:                  walletID,
		TargetAmount:                    100,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          0,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	otherOrderID := other.ID

	deliveryID := insertDelivery(t, pool, orderID, "delivered", nil)
	insertDelivery(t, pool, otherOrderID, "pending", nil) // must not leak into orderID's results

	_, err = pool.Exec(context.Background(), `
		INSERT INTO webhook_delivery_attempts (delivery_id, attempt_no, http_status, response_body, triggered_by)
		VALUES ($1, 1, 500, 'server error', 'auto'), ($1, 2, 200, 'ok', 'auto')
	`, deliveryID)
	if err != nil {
		t.Fatalf("insert webhook_delivery_attempts: %v", err)
	}

	got, err := ListForOrder(context.Background(), pool, orderID)
	if err != nil {
		t.Fatalf("ListForOrder() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	d := got[0]
	if d.ID != deliveryID || d.Status != "delivered" {
		t.Errorf("delivery = %+v, want id=%d status=delivered", d, deliveryID)
	}
	if len(d.Attempts) != 2 {
		t.Fatalf("len(Attempts) = %d, want 2", len(d.Attempts))
	}
	if d.Attempts[0].AttemptNo != 1 || d.Attempts[0].HTTPStatus == nil || *d.Attempts[0].HTTPStatus != 500 {
		t.Errorf("Attempts[0] = %+v, want attempt_no=1 http_status=500", d.Attempts[0])
	}
	if d.Attempts[1].AttemptNo != 2 || d.Attempts[1].HTTPStatus == nil || *d.Attempts[1].HTTPStatus != 200 {
		t.Errorf("Attempts[1] = %+v, want attempt_no=2 http_status=200", d.Attempts[1])
	}
}
