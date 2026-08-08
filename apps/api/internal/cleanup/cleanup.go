// Package cleanup runs periodic background tasks that prune stale rows.
package cleanup

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/albumblob"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const abandonedUploadAge = 7 * 24 * time.Hour

// retiredAttachmentRetention is how long a soft-deleted attachment row is kept
// after the event or calendar it belonged to went away. The row carries the
// filename and uploader the activity history reads, and it pins the blob
// through a RESTRICT foreign key, so this is also the delay before the bytes
// can be reclaimed.
const retiredAttachmentRetention = 30 * 24 * time.Hour

// avatarUploadListBatchSize must match the LIMIT in avatar_uploads.sql's
// ListExpiredAvatarUploads query.
const avatarUploadListBatchSize = 500

// storageSweepBatchSize bounds one pass over unreferenced blobs.
const storageSweepBatchSize = 500

// tokenSweepBatchSize bounds one DELETE over a table of expiring credentials.
// These tables grow with traffic rather than with content -- every sign-in
// adds a session -- so an unbounded statement would lock a backlog whose size
// nothing in the product controls.
const tokenSweepBatchSize = 500

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

// RunOnce performs a single cleanup pass. Run calls it on a ticker; it is
// exported so a test can drive one pass and observe what it collected instead
// of waiting on a timer.
func RunOnce(ctx context.Context, q *generated.Queries, storageClient *storage.Client) {
	runOnce(ctx, q, storageClient)
}

func runOnce(ctx context.Context, q *generated.Queries, storageClient *storage.Client) {
	now := time.Now()
	drainExpired(ctx, "expired password resets", func(ctx context.Context) (sql.Result, error) {
		return q.DeleteExpiredPasswordResets(ctx, generated.DeleteExpiredPasswordResetsParams{
			ExpiresAt: now,
			Limit:     tokenSweepBatchSize,
		})
	})
	drainExpired(ctx, "expired sign-in states", func(ctx context.Context) (sql.Result, error) {
		return q.DeleteExpiredSigninStates(ctx, generated.DeleteExpiredSigninStatesParams{
			ExpiresAt: now,
			Limit:     tokenSweepBatchSize,
		})
	})
	// Expired sessions are removed rather than left revoked: the row's only
	// job is to answer "is this token still good", and a row past its expiry
	// already answers no through the query's own predicate.
	drainExpired(ctx, "expired sessions", func(ctx context.Context) (sql.Result, error) {
		return q.DeleteExpiredSessions(ctx, generated.DeleteExpiredSessionsParams{
			ExpiresAt: now,
			Limit:     tokenSweepBatchSize,
		})
	})
	cleanupAbandonedUploads(ctx, q, storageClient, now.Add(-abandonedUploadAge))
	backfillAlbumPhotoObjects(ctx, q, storageClient)
	sweepUnreferencedObjects(ctx, q, storageClient, now.Add(-abandonedUploadAge))
}

// drainExpired runs a bounded DELETE until it comes back short, which is what
// says the backlog is gone. No cursor is needed the way the storage sweeps
// need one: every row this removes stops matching the predicate, so a row it
// cannot delete does not exist and nothing can head every page.
//
// maxBatches bounds one tick regardless, so a statement that somehow keeps
// reporting a full batch cannot turn the cleanup goroutine into a spin.
func drainExpired(ctx context.Context, what string, deleteBatch func(context.Context) (sql.Result, error)) {
	for range maxBatches {
		res, err := deleteBatch(ctx)
		if err != nil {
			slog.Warn("cleanup: delete "+what+" failed", "error", err)
			return
		}
		affected, err := res.RowsAffected()
		if err != nil || affected < tokenSweepBatchSize {
			return
		}
	}
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
	//
	// The cursor is what keeps a row the delete cannot remove from heading
	// every page: without it the sweep re-reads the same failing rows until
	// its batch budget is spent and never reaches the rest of the backlog.
	var attachmentCursor uint32
	for range maxBatches {
		rows, err := q.ListAbandonedAttachments(ctx, generated.ListAbandonedAttachmentsParams{
			CreatedAt: olderThan,
			ID:        attachmentCursor,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list abandoned attachments failed", "error", err)
			break
		}
		for _, row := range rows {
			attachmentCursor = row.ID
			if err := q.DeleteAbandonedAttachment(ctx, row.ID); err != nil {
				slog.Warn("cleanup: delete abandoned attachment failed", "id", row.ID, "error", err)
			}
		}
		if len(rows) < storageSweepBatchSize {
			break
		}
	}

	// Attachment rows retired with their event or calendar released their
	// reference then, but the row itself still pins the object. Removing it
	// once the history no longer needs it is what lets the object sweep below
	// actually reclaim the bytes.
	var retiredCursor uint32
	retiredBefore := olderThan.Add(abandonedUploadAge).Add(-retiredAttachmentRetention)
	for range maxBatches {
		ids, err := q.ListRetiredAttachments(ctx, generated.ListRetiredAttachmentsParams{
			UpdatedAt: sql.NullTime{Time: retiredBefore, Valid: true},
			ID:        retiredCursor,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list retired attachments failed", "error", err)
			break
		}
		for _, id := range ids {
			retiredCursor = id
			if err := q.DeleteRetiredAttachment(ctx, id); err != nil {
				slog.Warn("cleanup: delete retired attachment failed", "id", id, "error", err)
			}
		}
		if len(ids) < storageSweepBatchSize {
			break
		}
	}

	// Album photos that went out of use -- an upload that never landed, a photo
	// the user deleted, or one that went with its calendar. The row is dropped
	// first and the bytes only after, the same order the object sweep uses: a
	// key can be claimed by a storage object that other photos resolve
	// through, and taking the bytes out from under it would blank theirs.
	var photoCursor uint32
	for range maxBatches {
		photos, err := q.ListAbandonedAlbumPhotoStorageKeys(ctx, generated.ListAbandonedAlbumPhotoStorageKeysParams{
			UpdatedAt: sql.NullTime{Time: olderThan, Valid: true},
			ID:        photoCursor,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list abandoned album objects failed", "error", err)
			break
		}
		for _, photo := range photos {
			photoCursor = photo.ID
			res, err := q.DeleteAbandonedAlbumPhoto(ctx, photo.ID)
			if err != nil {
				slog.Warn("cleanup: delete abandoned album photo failed", "id", photo.ID, "error", err)
				continue
			}
			if affected, err := res.RowsAffected(); err != nil || affected == 0 {
				continue
			}
			// Removing the row is what ends the photo's claim on its blob, so
			// the reference is released here rather than where the photo went
			// out of use. Every way of losing one -- deleted by its owner,
			// retired with its calendar, an upload that never landed -- passes
			// through this delete, and releasing at each of them instead would
			// be three chances to release twice or not at all.
			if photo.StorageObjectID.Valid {
				releaseStorageObject(ctx, q, storageClient, uint32(photo.StorageObjectID.Int32))
			}
			// The photo's own key is still checked, and still only deleted
			// when no object claims it. That covers a photo the backfill never
			// reached, and the second copy of bytes that deduplicated onto
			// another photo's object: the row shared, the bytes did not.
			if photo.StorageKey == "" {
				continue
			}
			if used, err := q.CountStorageObjectsByKey(ctx, photo.StorageKey); err != nil {
				slog.Warn("cleanup: count objects for album key failed", "key", photo.StorageKey, "error", err)
				continue
			} else if used > 0 {
				continue
			}
			if err := storageClient.DeleteObject(ctx, photo.StorageKey); err != nil {
				slog.Warn("cleanup: delete abandoned album object failed", "key", photo.StorageKey, "error", err)
			}
		}
		if len(photos) < storageSweepBatchSize {
			break
		}
	}

	// ListExpiredAvatarUploads caps each call at avatarUploadListBatchSize rows;
	// loop until a short batch confirms the backlog is drained, rather than
	// leaving anything beyond the first page for expensive objects (their
	// 7-day storage) to sit around until the next tick. maxBatches bounds one
	// cleanup run in case rows are somehow never removed (e.g. a persistent
	// delete failure), so this cannot spin forever.
	expiresBefore := olderThan.Add(abandonedUploadAge)
	var avatarCursor uint32
	for range maxBatches {
		expiredUploads, err := q.ListExpiredAvatarUploads(ctx, generated.ListExpiredAvatarUploadsParams{
			ExpiresAt: expiresBefore,
			ID:        avatarCursor,
		})
		if err != nil {
			slog.Warn("cleanup: list expired avatar uploads failed", "error", err)
			return
		}
		for _, upload := range expiredUploads {
			avatarCursor = upload.ID
			res, err := q.DeleteExpiredAvatarUpload(ctx, generated.DeleteExpiredAvatarUploadParams{
				ID:        upload.ID,
				ExpiresAt: expiresBefore,
			})
			if err != nil {
				slog.Warn("cleanup: delete expired avatar upload failed", "id", upload.ID, "error", err)
				continue
			}
			if affected, err := res.RowsAffected(); err != nil || affected == 0 {
				continue
			}
			// The key is derived from the user and the digest of the bytes, so
			// a second upload of the same picture reserves the same key as the
			// confirmed avatar already on display. Abandoning that second
			// reservation must not take the first one's bytes with it, which is
			// what deleting the blob unconditionally used to do.
			if used, err := q.CountStorageObjectsByKey(ctx, upload.StorageKey); err != nil {
				slog.Warn("cleanup: count objects for avatar key failed", "key", upload.StorageKey, "error", err)
				continue
			} else if used > 0 {
				continue
			}
			if err := storageClient.DeleteObject(ctx, upload.StorageKey); err != nil {
				slog.Warn("cleanup: delete abandoned avatar object failed", "key", upload.StorageKey, "error", err)
			}
		}
		if len(expiredUploads) < avatarUploadListBatchSize {
			return
		}
	}
}

// releaseStorageObject gives back one reference and, if it was the last one,
// reclaims the object in the same pass.
//
// Waiting for sweepUnreferencedObjects instead would put the bytes behind its
// age gate, which is counted from when the object row was created -- so a
// photo deleted today would wait out the album's own retention and then that
// gate again. The gate is there to protect an object between being reserved
// and being confirmed; one whose last referring row was just deleted is not
// in that state, and DeleteUnreferencedStorageObject re-checks both referrers
// before removing anything.
func releaseStorageObject(ctx context.Context, q *generated.Queries, storageClient *storage.Client, objectID uint32) {
	if err := q.DecrementStorageObjectRefs(ctx, objectID); err != nil {
		slog.Warn("cleanup: release storage object reference failed", "id", objectID, "error", err)
		return
	}
	object, err := q.GetStorageObjectByID(ctx, objectID)
	if err != nil {
		// Already collected, or never readable. Either way there is no key
		// left to act on.
		return
	}
	res, err := q.DeleteUnreferencedStorageObject(ctx, objectID)
	if err != nil {
		slog.Warn("cleanup: delete released storage object failed", "id", objectID, "error", err)
		return
	}
	// Anything else still holding it -- another photo, an attachment, a
	// reference taken since the count was read -- and the row stays, which is
	// what keeps the blob under it.
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		return
	}
	if err := storageClient.DeleteObject(ctx, object.StorageKey); err != nil {
		slog.Warn("cleanup: delete released object failed", "key", object.StorageKey, "error", err)
	}
}

// albumBackfillBatchSize and albumBackfillBatches bound one tick of the
// backfill far tighter than the other sweeps.
//
// Every other sweep here touches rows; this one reads each photo's bytes to
// digest them, so its cost per row is the size of the picture rather than the
// size of a row. A hundred photos a tick converges on any album this product
// realistically holds within a day, without a restart turning into hours of
// reading.
const (
	albumBackfillBatchSize = 50
	albumBackfillBatches   = 2
)

// backfillAlbumPhotoObjects moves photos that predate storage_object_id onto
// the object model, so the album stops being the one place with a blob nothing
// counts.
//
// It is a sweep rather than a one-shot command because a command's effect is
// "somebody ran it": a deployment where nobody did looks exactly like one
// where the migration is finished. This converges on its own and then costs
// one empty query per tick.
//
// A photo whose bytes cannot be read is left alone, on its own key, where it
// goes on being served. That is the whole point of keeping storage_key: this
// pass failing does not take a photo off the album.
func backfillAlbumPhotoObjects(ctx context.Context, q *generated.Queries, storageClient *storage.Client) {
	if storageClient == nil {
		return
	}
	var cursor uint32
	for range albumBackfillBatches {
		photos, err := q.ListAlbumPhotosWithoutStorageObject(ctx, generated.ListAlbumPhotosWithoutStorageObjectParams{
			ID:    cursor,
			Limit: albumBackfillBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list album photos without a storage object failed", "error", err)
			return
		}
		for _, photo := range photos {
			cursor = photo.ID
			digest, err := storageClient.SHA256(ctx, photo.StorageKey)
			if err != nil {
				slog.Warn("cleanup: digest album photo for backfill failed",
					"id", photo.ID, "key", photo.StorageKey, "error", err)
				continue
			}
			if err := albumblob.Attach(ctx, q, albumblob.Photo{
				ID:          photo.ID,
				WorkspaceID: photo.WorkspaceID,
				StorageKey:  photo.StorageKey,
				ContentType: photo.ContentType,
				ByteSize:    photo.ByteSize,
				SHA256:      digest,
			}); err != nil {
				slog.Warn("cleanup: attach album photo to storage object failed", "id", photo.ID, "error", err)
			}
		}
		if len(photos) < albumBackfillBatchSize {
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
	// Objects are walked by id. An object an attachment row still points at
	// cannot be deleted (the foreign key is RESTRICT), and re-reading from the
	// head would put that same object at the front of every page.
	var cursor uint32
	for range maxBatches {
		objects, err := q.ListUnreferencedStorageObjects(ctx, generated.ListUnreferencedStorageObjectsParams{
			CreatedAt: olderThan,
			ID:        cursor,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list unreferenced storage objects failed", "error", err)
			return
		}
		for _, obj := range objects {
			cursor = obj.ID
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
