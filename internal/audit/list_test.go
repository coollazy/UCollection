package audit

import (
	"context"
	"testing"
	"time"
)

func TestList_FiltersAndPagination(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	targetType := "order"
	id1 := int64(1)
	id2 := int64(2)
	if err := Log(ctx, pool, "admin", "LOGIN_SUCCESS", nil, nil, nil); err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if err := Log(ctx, pool, "admin", "ORDER_MANUAL_CREATED", &targetType, &id1, map[string]any{"note": "a"}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if err := Log(ctx, pool, "admin", "ORDER_MANUAL_CREATED", &targetType, &id2, map[string]any{"note": "b"}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	entries, total, err := List(ctx, pool, Filter{ActionType: "ORDER_MANUAL_CREATED"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("total=%d len(entries)=%d, want 2/2", total, len(entries))
	}
	// Newest first.
	if entries[0].TargetID == nil || *entries[0].TargetID != 2 {
		t.Errorf("entries[0].TargetID = %v, want 2 (newest first)", entries[0].TargetID)
	}
	if entries[0].Detail["note"] != "b" {
		t.Errorf("entries[0].Detail[note] = %v, want b", entries[0].Detail["note"])
	}

	entries, total, err = List(ctx, pool, Filter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 3 || len(entries) != 3 {
		t.Fatalf("unfiltered total=%d len(entries)=%d, want 3/3", total, len(entries))
	}
	if entries[2].Detail != nil {
		t.Errorf("LOGIN_SUCCESS entry detail = %v, want nil", entries[2].Detail)
	}

	entries, total, err = List(ctx, pool, Filter{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 3 || len(entries) != 1 {
		t.Fatalf("paginated total=%d len(entries)=%d, want 3/1", total, len(entries))
	}
}

func TestList_TimeRangeFilter(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := Log(ctx, pool, "admin", "LOGOUT", nil, nil, nil); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	future := time.Now().Add(time.Hour)
	entries, total, err := List(ctx, pool, Filter{CreatedFrom: future})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 0 || len(entries) != 0 {
		t.Fatalf("future CreatedFrom should exclude everything, got total=%d len=%d", total, len(entries))
	}

	past := time.Now().Add(-time.Hour)
	entries, total, err = List(ctx, pool, Filter{CreatedFrom: past})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("past CreatedFrom should include the row, got total=%d len=%d", total, len(entries))
	}
}
