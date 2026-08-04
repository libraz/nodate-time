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

// avatarUploadListBatchSize must match the LIMIT in avatar_uploads.sql's
// ListExpiredAvatarUploads query.
const avatarUploadListBatchSize = 500

// storageSweepBatchSize bounds one pass over unreferenced blobs.
const storageSweepBatchSize = 500

// maxBatches bounds how many pages a single cleanup tick will drain, so a
// persistent per-row failure cannot turn this into an infinite loop.
const maxBatches = 1000

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
	if err := q.DeleteExpiredSigninStates(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired sign-in states failed", "error", err)
	}
	// Expired sessions are removed rather than left revoked: the row's only
	// job is to answer "is this token still good", and a row past its expiry
	// already answers no through the query's own predicate.
	if err := q.DeleteExpiredSessions(ctx, now); err != nil {
		slog.Warn("cleanup: delete expired sessions failed", "error", err)
	}
	cleanupAbandonedUploads(ctx, q, storageClient, now.Add(-abandonedUploadAge))
	sweepUnreferencedObjects(ctx, q, storageClient, now.Add(-abandonedUploadAge))
}

func cleanupAbandonedUploads(ctx context.Context, q *generated.Queries, storageClient *storage.Client, olderThan time.Time) {
	if storageClient == nil {
		return
	}

	// An attachment reservation never took a reference on its blob -- the
	// reference is taken when the upload is confirmed. So dropping the row
	// is all that happens here; the blob it pointed at falls to ref_count
	// zero and the object sweep below collects it along with every other
	// unreferenced one.
	for range maxBatches {
		rows, err := q.ListAbandonedAttachments(ctx, generated.ListAbandonedAttachmentsParams{
			CreatedAt: olderThan,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list abandoned attachments failed", "error", err)
			break
		}
		for _, row := range rows {
			if err := q.DeleteAbandonedAttachment(ctx, row.ID); err != nil {
				slog.Warn("cleanup: delete abandoned attachment failed", "id", row.ID, "error", err)
			}
		}
		if len(rows) < storageSweepBatchSize {
			break
		}
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

	// ListExpiredAvatarUploads caps each call at avatarUploadListBatchSize rows;
	// loop until a short batch confirms the backlog is drained, rather than
	// leaving anything beyond the first page for expensive objects (their
	// 7-day storage) to sit around until the next tick. maxBatches bounds one
	// cleanup run in case rows are somehow never removed (e.g. a persistent
	// delete failure), so this cannot spin forever.
	expiresBefore := olderThan.Add(abandonedUploadAge)
	for range maxBatches {
		expiredUploads, err := q.ListExpiredAvatarUploads(ctx, expiresBefore)
		if err != nil {
			slog.Warn("cleanup: list expired avatar uploads failed", "error", err)
			return
		}
		for _, upload := range expiredUploads {
			deleteObjects(ctx, storageClient, []string{upload.StorageKey}, func(ctx context.Context, _ string) error {
				_, err := q.DeleteExpiredAvatarUpload(ctx, generated.DeleteExpiredAvatarUploadParams{
					ID:        upload.ID,
					ExpiresAt: expiresBefore,
				})
				return err
			})
		}
		if len(expiredUploads) < avatarUploadListBatchSize {
			return
		}
	}
}

// sweepUnreferencedObjects removes blobs nothing points at any more.
//
// The age cutoff is what makes this safe against a reservation in flight:
// a freshly created object legitimately sits at ref_count zero between
// being reserved and being confirmed, and deleting it in that window would
// break an upload that was about to succeed.
func sweepUnreferencedObjects(ctx context.Context, q *generated.Queries, storageClient *storage.Client, olderThan time.Time) {
	if storageClient == nil {
		return
	}
	for range maxBatches {
		objects, err := q.ListUnreferencedStorageObjects(ctx, generated.ListUnreferencedStorageObjectsParams{
			CreatedAt: olderThan,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list unreferenced storage objects failed", "error", err)
			return
		}
		for _, obj := range objects {
			// Drop the row first. DeleteStorageObject re-checks ref_count = 0,
			// so an object that gained a reference since it was listed reports
			// no affected rows and its blob is left alone. Deleting the blob
			// first would leave that winner pointing at nothing.
			res, err := q.DeleteStorageObject(ctx, obj.ID)
			if err != nil {
				slog.Warn("cleanup: delete storage object row failed", "id", obj.ID, "error", err)
				continue
			}
			if affected, err := res.RowsAffected(); err != nil || affected == 0 {
				continue
			}
			if err := storageClient.DeleteObject(ctx, obj.StorageKey); err != nil {
				slog.Warn("cleanup: delete unreferenced object failed", "key", obj.StorageKey, "error", err)
			}
		}
		if len(objects) < storageSweepBatchSize {
			return
		}
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
