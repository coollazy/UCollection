// Package store is the DB access layer (connection pool + migrations). See
// docs/開發流程框架-03-技術架構設計.md 第2節.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps the application's PostgreSQL connection pool.
type Pool struct {
	*pgxpool.Pool
}

// Open creates a connection pool and verifies connectivity with a ping.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	return &Pool{Pool: pool}, nil
}
