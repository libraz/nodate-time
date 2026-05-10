// Package cleanup runs periodic background tasks that prune stale rows.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Run starts a goroutine that periodically deletes expired tokens.
// It returns immediately and runs until ctx is canceled.
func Run(ctx context.Context, q *generated.Queries, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Run once on startup so a long-running process eventually cleans up
		// even if it is restarted before the first tick.
		runOnce(ctx, q)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce(ctx, q)
			}
		}
	}()
}

func runOnce(ctx context.Context, q *generated.Queries) {
	now := time.Now()
	if err := q.DeleteExpiredPasswordResets(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired password resets failed", "error", err)
	}
	if err := q.DeleteExpiredOAuthStates(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired oauth states failed", "error", err)
	}
}
