package auth

import (
	"context"
	"log"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// sessionCleanupInterval is its own lightweight ticker rather than being
// folded into 模組4任務1 (that task is scoped specifically to the order
// state machine's PENDING/CONFIRMING gating, 見第4節) — mirrors the
// independent-ticker precedent set by 第8節 webhook worker's main queue
// poll. Missing a sweep has no security consequence (expired sessions are
// already rejected by lookupActiveSession's own checks); this is pure DB
// housekeeping (技術架構設計第9節「Session模型」).
const sessionCleanupInterval = 30 * time.Second

func runSessionCleanupTask(ctx context.Context, pool *store.Pool) {
	for {
		if _, err := pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`); err != nil {
			log.Printf("auth: session cleanup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sessionCleanupInterval):
		}
	}
}
