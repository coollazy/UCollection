package store

import (
	"context"
	"os"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping test that needs a real PostgreSQL instance")
	}

	if err := Migrate(context.Background(), databaseURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	pool, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()
}
