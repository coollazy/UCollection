package audit

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/coollazy/UCollection/internal/store"
)

func testPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping test that needs a real PostgreSQL instance")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetDB(t *testing.T, pool *store.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE audit_logs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

func TestLog_WithDetail(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	targetType := "order"
	targetID := int64(42)
	if err := Log(ctx, pool, "admin", "LOGIN_SUCCESS", &targetType, &targetID, map[string]any{"ip": "127.0.0.1"}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	var actor, actionType, gotTargetType string
	var gotTargetID int64
	var detailRaw []byte
	err := pool.QueryRow(ctx, `SELECT actor, action_type, target_type, target_id, detail FROM audit_logs`).
		Scan(&actor, &actionType, &gotTargetType, &gotTargetID, &detailRaw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if actor != "admin" || actionType != "LOGIN_SUCCESS" || gotTargetType != "order" || gotTargetID != 42 {
		t.Fatalf("unexpected row: actor=%s action_type=%s target_type=%s target_id=%d", actor, actionType, gotTargetType, gotTargetID)
	}
	var detail map[string]any
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail["ip"] != "127.0.0.1" {
		t.Fatalf("detail[ip] = %v, want 127.0.0.1", detail["ip"])
	}
}

func TestLog_NilDetailAndTargets(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := Log(ctx, pool, "admin", "LOGOUT", nil, nil, nil); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	var actionType string
	var targetType *string
	var targetID *int64
	var detail *string
	err := pool.QueryRow(ctx, `SELECT action_type, target_type, target_id, detail FROM audit_logs`).
		Scan(&actionType, &targetType, &targetID, &detail)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if actionType != "LOGOUT" || targetType != nil || targetID != nil || detail != nil {
		t.Fatalf("expected nulls, got target_type=%v target_id=%v detail=%v", targetType, targetID, detail)
	}
}
