// Package cleanup runs periodic background tasks that prune stale rows.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const abandonedUploadAge = 7 * 24 * time.Hour

// Run starts a goroutine that periodically deletes expired tokens.
// It returns immediately and runs until ctx is canceled.
func Run(ctx context.Context, q *generated.Queries, storageClient *storage.Client, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Run once on startup so a long-running process eventually cleans up
		// even if it is restarted before the first tick.
		runOnce(ctx, q, storageClient)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce(ctx, q, storageClient)
			}
		}
	}()
}

func runOnce(ctx context.Context, q *generated.Queries, storageClient *storage.Client) {
	now := time.Now()
	if err := q.DeleteExpiredPasswordResets(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired password resets failed", "error", err)
	}
	if err := q.DeleteExpiredOAuthStates(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired oauth states failed", "error", err)
	}
	cleanupAbandonedUploads(ctx, q, storageClient, now.Add(-abandonedUploadAge))
}

func cleanupAbandonedUploads(ctx context.Context, q *generated.Queries, storageClient *storage.Client, olderThan time.Time) {
	if storageClient != nil {
		if keys, err := q.ListAbandonedAttachmentStorageKeys(ctx, olderThan); err != nil {
			slog.Warn("cleanup: list abandoned attachment objects failed", "error", err)
		} else {
			deleteObjects(ctx, storageClient, keys)
		}
		if keys, err := q.ListAbandonedAlbumPhotoStorageKeys(ctx, olderThan); err != nil {
			slog.Warn("cleanup: list abandoned album objects failed", "error", err)
		} else {
			deleteObjects(ctx, storageClient, keys)
		}
	}
	if err := q.DeleteAbandonedAttachments(ctx, olderThan); err != nil {
		slog.Warn("cleanup: delete abandoned attachment rows failed", "error", err)
	}
	if err := q.DeleteAbandonedAlbumPhotos(ctx, olderThan); err != nil {
		slog.Warn("cleanup: delete abandoned album rows failed", "error", err)
	}
}

func deleteObjects(ctx context.Context, storageClient *storage.Client, keys []string) {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := storageClient.DeleteObject(ctx, key); err != nil {
			slog.Warn("cleanup: delete abandoned object failed", "key", key, "error", err)
		}
	}
}
