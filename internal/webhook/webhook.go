package webhook

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

const (
	workerTickerPeriod         = 10 * time.Second
	awaitingConfigTickerPeriod = 1 * time.Hour
	pickBatchLimit             = 20
	// sendingTimeout (2 minutes) is hardcoded directly into pickBatch's SQL
	// (see worker.go) rather than referenced from here, since it can't be
	// passed as a query parameter — kept as a named constant only for
	// documentation purposes; changing it means updating the SQL literal too.
	retryBaseDelay = 1 * time.Minute
	retryMaxDelay  = 6 * time.Hour
	retryWindow    = 24 * time.Hour
)

// Deps are the dependencies Run needs.
type Deps struct {
	Pool       *store.Pool
	HTTPClient *http.Client
}

// Run starts the two always-on background tasks (技術架構設計第8節): the
// delivery worker (10秒輪詢) and the awaiting_config sweep (每小時檢查一次).
// Mirrors internal/scanner.Run's orchestration pattern — a failure in one
// cancels the other.
func Run(ctx context.Context, deps Deps) error {
	if deps.HTTPClient == nil {
		deps.HTTPClient = newHTTPClient()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	run := func(name string, task func(context.Context, Deps) error) {
		defer wg.Done()
		if err := task(ctx, deps); err != nil {
			log.Printf("webhook: task %s stopped: %v", name, err)
			errs <- fmt.Errorf("webhook: task %s: %w", name, err)
			cancel()
			return
		}
		errs <- nil
	}

	wg.Add(2)
	go run("worker", runWorkerTask)
	go run("awaiting_config", runAwaitingConfigTask)

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
