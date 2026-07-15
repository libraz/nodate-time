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
	if storageClient == nil {
		return
	}

	if keys, err := q.ListAbandonedAttachmentStorageKeys(ctx, olderThan); err != nil {
		slog.Warn("cleanup: list abandoned attachment objects failed", "error", err)
	} else {
		deleteObjects(ctx, storageClient, keys, func(ctx context.Context, key string) error {
			_, err := q.DeleteAbandonedAttachmentByStorageKey(ctx, generated.DeleteAbandonedAttachmentByStorageKeyParams{
				StorageKey: key,
				CreatedAt:  olderThan,
			})
			return err
		})
	}
	if keys, err := q.ListAbandonedAlbumPhotoStorageKeys(ctx, olderThan); err != nil {
		slog.Warn("cleanup: list abandoned album objects failed", "error", err)
	} else {
		deleteObjects(ctx, storageClient, keys, func(ctx context.Context, key string) error {
			_, err := q.DeleteAbandonedAlbumPhotoByStorageKey(ctx, generated.DeleteAbandonedAlbumPhotoByStorageKeyParams{
				StorageKey: key,
				CreatedAt:  olderThan,
			})
			return err
		})
	}

	expiredUploads, err := q.ListExpiredAvatarUploads(ctx, olderThan.Add(abandonedUploadAge))
	if err != nil {
		slog.Warn("cleanup: list expired avatar uploads failed", "error", err)
		return
	}
	for _, upload := range expiredUploads {
		deleteObjects(ctx, storageClient, []string{upload.StorageKey}, func(ctx context.Context, _ string) error {
			_, err := q.DeleteExpiredAvatarUpload(ctx, generated.DeleteExpiredAvatarUploadParams{
				ID:        upload.ID,
				ExpiresAt: olderThan.Add(abandonedUploadAge),
			})
			return err
		})
	}
}

type objectDeleter interface {
	DeleteObject(context.Context, string) error
}

func deleteObjects(ctx context.Context, storageClient objectDeleter, keys []string, deleteRow func(context.Context, string) error) {
	for _, key := range keys {
		if key != "" {
			if err := storageClient.DeleteObject(ctx, key); err != nil {
				slog.Warn("cleanup: delete abandoned object failed", "key", key, "error", err)
				continue
			}
		}
		if err := deleteRow(ctx, key); err != nil {
			slog.Warn("cleanup: delete abandoned upload row failed", "key", key, "error", err)
		}
	}
}
