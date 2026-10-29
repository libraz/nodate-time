// Package cleanup runs periodic background tasks that prune stale rows.
package cleanup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/libraz/nodate-time/apps/api/internal/albumblob"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const abandonedUploadAge = 7 * 24 * time.Hour

// retiredAttachmentRetention is how long a soft-deleted attachment row is kept
// after the event or calendar it belonged to went away. The row carries the
// filename and uploader the activity history reads, and it holds its reference
// on the blob until it is removed, so this is also the delay before the bytes
// can be reclaimed.
const retiredAttachmentRetention = 30 * 24 * time.Hour

// avatarUploadListBatchSize must match the LIMIT in avatar_uploads.sql's
// ListExpiredAvatarUploads query.
const avatarUploadListBatchSize = 500

// storageSweepBatchSize bounds one pass over unreferenced blobs.
const storageSweepBatchSize = 500

// reservationTTL is how long an object may sit unconfirmed before the
// unconfirmed sweep reclaims it: the 15-minute upload URL plus room for a
// transfer in flight, at the 45-minute floor PROTOCOL.md section Storage sets
// so no product's sweep takes another's upload in progress.
const reservationTTL = 45 * time.Minute

// tokenSweepBatchSize bounds one DELETE over a table of expiring credentials.
// These tables grow with traffic rather than with content -- every sign-in
// adds a session -- so an unbounded statement would lock a backlog whose size
// nothing in the product controls.
const tokenSweepBatchSize = 500

// maxBatches bounds how many pages a single cleanup tick will drain, so a
// persistent per-row failure cannot turn this into an infinite loop.
const maxBatches = 1000

// DB is what the sweeps run on: plain statements, and transactions for the
// ones whose writes must commit together. *sql.DB is the one in production.
type DB interface {
	generated.DBTX
	dbtx.Beginner
}

// Run starts a goroutine that periodically deletes expired tokens.
// It returns immediately and runs until ctx is canceled.
//
// Blobs are removed only once the configured bucket matches the one the
// database records (blobref.LocationAllowsRemoval, decided once per process).
// On a mismatch the removal queue is left alone and the error logged on every
// tick; the row sweeps still run, since they only queue keys.
func Run(ctx context.Context, db DB, storageClient *storage.Client, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// The cursor outlives the tick so the backfill resumes past rows it
		// could not process instead of re-reading them first every time.
		var albumCursor uint32
		drain := func(ctx context.Context, q *generated.Queries) bool {
			if storageClient == nil {
				return false
			}
			if err := blobref.LocationAllowsRemoval(ctx, q, storageClient.Location()); err != nil {
				slog.Error("cleanup: storage location check failed; blob removal stopped", "error", err)
				return false
			}
			return true
		}
		// Run once on startup so a long-running process eventually cleans up
		// even if it is restarted before the first tick.
		runOnce(ctx, db, storageClient, &albumCursor, drain)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce(ctx, db, storageClient, &albumCursor, drain)
			}
		}
	}()
}

// RunOnce performs a single cleanup pass. Run calls it on a ticker; it is
// exported so a test can drive one pass and observe what it collected instead
// of waiting on a timer. The caller vouches for storageClient's bucket: the
// pass drains the removal queue without checking the recorded location.
func RunOnce(ctx context.Context, db DB, storageClient *storage.Client) {
	var albumCursor uint32
	runOnce(ctx, db, storageClient, &albumCursor, func(context.Context, *generated.Queries) bool { return true })
}

func runOnce(ctx context.Context, db DB, storageClient *storage.Client, albumCursor *uint32, drain func(context.Context, *generated.Queries) bool) {
	q := generated.New(db)
	now := time.Now()
	drainExpired(ctx, "expired password resets", func(ctx context.Context) (sql.Result, error) {
		return q.DeleteExpiredPasswordResets(ctx, generated.DeleteExpiredPasswordResetsParams{
			ExpiresAt: now,
			Limit:     tokenSweepBatchSize,
		})
	})
	drainExpired(ctx, "expired email verifications", func(ctx context.Context) (sql.Result, error) {
		return q.DeleteExpiredEmailVerifications(ctx, generated.DeleteExpiredEmailVerificationsParams{
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
	cleanupAbandonedUploads(ctx, db, q, storageClient, now.Add(-abandonedUploadAge))
	// Reservations go before the backfill: a photo is left unattached while a
	// reservation of its digest waits at another key, and reclaiming that
	// reservation is what lets the backfill attach it.
	sweepUnconfirmedObjects(ctx, db, q, storageClient, now.Add(-reservationTTL))
	backfillAlbumPhotoObjects(ctx, db, q, storageClient, albumCursor)
	sweepUnreferencedObjects(ctx, db, q, storageClient)
	if drain(ctx, q) {
		drainBlobRemovals(ctx, q, storageClient)
	}
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

func cleanupAbandonedUploads(ctx context.Context, db DB, q *generated.Queries, storageClient *storage.Client, olderThan time.Time) {
	if storageClient == nil {
		return
	}

	// An attachment reservation holds a reference on its object from the
	// moment it is created, so dropping it gives the reference back in the
	// same transaction. The object itself is left to the sweeps below: an
	// unconfirmed one to the unconfirmed sweep, a confirmed one nothing else
	// holds to the unreferenced sweep.
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
			if err := dbtx.Run(ctx, db, func(tq *generated.Queries) error {
				res, err := tq.DeleteAbandonedAttachment(ctx, row.ID)
				if err != nil {
					return err
				}
				if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
					return err
				}
				if err := blobref.Release(ctx, tq, row.StorageObjectID); err != nil {
					return err
				}
				// A reservation may have uploaded its own copy of the bytes to a
				// key only it names. The row is gone, so nothing else can reach
				// them.
				return blobref.QueueRemoval(ctx, tq, attachmentUploadKey(row.WorkspacePublicID, row.PublicID))
			}); err != nil {
				slog.Warn("cleanup: delete abandoned attachment failed", "id", row.ID, "error", err)
			}
		}
		if len(rows) < storageSweepBatchSize {
			break
		}
	}

	// Attachment rows retired with their event or calendar keep their
	// reference for as long as they exist. Removing one once the history no
	// longer needs it gives the reference back, which is what lets the
	// unreferenced sweep below reclaim the bytes.
	var retiredCursor uint32
	retiredBefore := olderThan.Add(abandonedUploadAge).Add(-retiredAttachmentRetention)
	for range maxBatches {
		rows, err := q.ListRetiredAttachments(ctx, generated.ListRetiredAttachmentsParams{
			UpdatedAt: sql.NullTime{Time: retiredBefore, Valid: true},
			ID:        retiredCursor,
			Limit:     storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list retired attachments failed", "error", err)
			break
		}
		for _, row := range rows {
			retiredCursor = row.ID
			if err := dbtx.Run(ctx, db, func(tq *generated.Queries) error {
				res, err := tq.DeleteRetiredAttachment(ctx, row.ID)
				if err != nil {
					return err
				}
				if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
					return err
				}
				return blobref.Release(ctx, tq, row.StorageObjectID)
			}); err != nil {
				slog.Warn("cleanup: delete retired attachment failed", "id", row.ID, "error", err)
			}
		}
		if len(rows) < storageSweepBatchSize {
			break
		}
	}

	// Album photos that went out of use -- an upload that never landed, a photo
	// the user deleted, or one that went with its calendar.
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
			// Removing the row is what ends the photo's claim on its blobs, so
			// the references are released in the same transaction: a failure
			// between the two leaves the row and the counts as they were, and
			// the next tick retries. Every way of losing a photo -- deleted by
			// its owner, retired with its calendar, an upload that never
			// landed -- passes through this delete, and releasing at each of
			// them instead would be three chances to release twice or not at
			// all.
			//
			// A photo holds up to two: the picture and its grid-sized
			// rendering. Releasing only the first would leave the thumbnail's
			// object at a count nothing will ever give back, pinning bytes no
			// row can reach.
			err := dbtx.Run(ctx, db, func(tq *generated.Queries) error {
				res, err := tq.DeleteAbandonedAlbumPhoto(ctx, photo.ID)
				if err != nil {
					return err
				}
				if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
					return err
				}
				for _, id := range []sql.NullInt32{photo.StorageObjectID, photo.ThumbnailObjectID} {
					if !id.Valid {
						continue
					}
					if err := releaseStorageObject(ctx, tq, uint32(id.Int32)); err != nil {
						return err
					}
				}
				// The photo's own keys are queued too. The drainer leaves a key
				// a confirmed object still claims, which covers the second copy
				// of bytes that deduplicated onto another photo's object: the
				// row was shared, the bytes were not.
				if photo.StorageKey == "" {
					return nil
				}
				return blobref.QueueRemoval(ctx, tq, photo.StorageKey, albumblob.ThumbnailKey(photo.StorageKey))
			})
			if err != nil {
				slog.Warn("cleanup: delete abandoned album photo failed", "id", photo.ID, "error", err)
			}
		}
		if len(photos) < storageSweepBatchSize {
			break
		}
	}

	// ListExpiredAvatarUploads caps each call at avatarUploadListBatchSize rows;
	// loop until a short batch confirms the backlog is drained. maxBatches
	// bounds one cleanup run in case rows are somehow never removed (e.g. a
	// persistent delete failure), so this cannot spin forever.
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
			// The key is derived from the user and the digest of the bytes, so
			// a second upload of the same picture reserves the same key as the
			// confirmed avatar already on display. The drainer leaves a key a
			// confirmed object claims, so abandoning that second reservation
			// does not take the first one's bytes with it.
			if err := dbtx.Run(ctx, db, func(tq *generated.Queries) error {
				res, err := tq.DeleteExpiredAvatarUpload(ctx, generated.DeleteExpiredAvatarUploadParams{
					ID:        upload.ID,
					ExpiresAt: expiresBefore,
				})
				if err != nil {
					return err
				}
				if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
					return err
				}
				return blobref.QueueRemoval(ctx, tq, upload.StorageKey)
			}); err != nil {
				slog.Warn("cleanup: delete expired avatar upload failed", "id", upload.ID, "error", err)
			}
		}
		if len(expiredUploads) < avatarUploadListBatchSize {
			return
		}
	}
}

// attachmentUploadKey is the key one attachment reservation uploads its own
// copy of the bytes to.
func attachmentUploadKey(workspacePublicID, attachmentPublicID []byte) string {
	return fmt.Sprintf("workspace/%s/uploads/%s", publicIDString(workspacePublicID), publicIDString(attachmentPublicID))
}

func publicIDString(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return fmt.Sprintf("%x", b)
	}
	return u.String()
}

// releaseStorageObject gives back one reference and, if it was the last one,
// removes the object's row and queues its blob in the same transaction.
//
// Waiting for sweepUnreferencedObjects instead would leave the row to the next
// pass for no reason: DeleteUnreferencedStorageObject re-checks every column
// that can point at an object before removing anything. A RESTRICT refusal
// means a layer this product does not own still holds the object, and it
// stays.
func releaseStorageObject(ctx context.Context, q *generated.Queries, objectID uint32) error {
	if err := blobref.Release(ctx, q, objectID); err != nil {
		return err
	}
	object, err := q.GetStorageObjectByID(ctx, objectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Already collected, so there is no key left to act on.
			return nil
		}
		return err
	}
	res, err := q.DeleteUnreferencedStorageObject(ctx, objectID)
	if blobref.IsForeignKeyRestrict(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Anything else still holding it -- another photo, an attachment, a
	// reference taken since the count was read -- and the row stays, which is
	// what keeps the blob under it.
	if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
		return err
	}
	return blobref.QueueRemoval(ctx, q, object.StorageKey)
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
//
// cursor is where the previous tick stopped. A tick that read from the start
// would spend its whole budget on the same unreadable photos for as long as
// they exist, so the pass resumes after the last row it looked at and wraps to
// the start only once it has seen the end of the list.
func backfillAlbumPhotoObjects(ctx context.Context, db DB, q *generated.Queries, storageClient *storage.Client, cursor *uint32) {
	if storageClient == nil {
		return
	}
	for range albumBackfillBatches {
		photos, err := q.ListAlbumPhotosWithoutStorageObject(ctx, generated.ListAlbumPhotosWithoutStorageObjectParams{
			ID:    *cursor,
			Limit: albumBackfillBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list album photos without a storage object failed", "error", err)
			return
		}
		for _, photo := range photos {
			*cursor = photo.ID
			digest, err := storageClient.SHA256(ctx, photo.StorageKey)
			if err != nil {
				slog.Warn("cleanup: digest album photo for backfill failed",
					"id", photo.ID, "key", photo.StorageKey, "error", err)
				continue
			}
			// The attach runs in one transaction: the read of an unverified
			// reservation it takes over is only held against a concurrent
			// confirm for as long as that transaction is open.
			if err := dbtx.Run(ctx, db, func(tq *generated.Queries) error {
				return albumblob.Attach(ctx, tq, albumblob.Photo{
					ID:          photo.ID,
					WorkspaceID: photo.WorkspaceID,
					StorageKey:  photo.StorageKey,
					ContentType: photo.ContentType,
					ByteSize:    photo.ByteSize,
					SHA256:      digest,
				})
			}); err != nil {
				slog.Warn("cleanup: attach album photo to storage object failed", "id", photo.ID, "error", err)
			}
		}
		if len(photos) < albumBackfillBatchSize {
			*cursor = 0
			return
		}
	}
}

// candidate is one object a sweep listed for reclaiming.
type candidate struct {
	id  uint32
	key string
}

// pageByID hands every listed candidate to reclaim, a page at a time in id
// order, until a page comes back short. Each page starts after the last id
// seen, whatever reclaim did with it: an object that refuses to go -- one
// another layer still holds, a delete that failed -- would otherwise head a
// page read from the start every time, and the sweep would spend its whole
// budget on it without reaching the backlog behind.
func pageByID(ctx context.Context, what string, list func(afterID uint32) ([]candidate, error), reclaim func(candidate)) {
	var cursor uint32
	for range maxBatches {
		page, err := list(cursor)
		if err != nil {
			slog.Warn("cleanup: list "+what+" failed", "error", err)
			return
		}
		for _, c := range page {
			if ctx.Err() != nil {
				return
			}
			cursor = c.id
			reclaim(c)
		}
		if len(page) < storageSweepBatchSize {
			return
		}
	}
}

// logReclaim reports what a reclaim transaction ended with. A RESTRICT refusal
// is a layer this product does not own still holding the object: skipped, not
// a failure of the pass.
func logReclaim(what string, id uint32, err error) {
	switch {
	case err == nil, errors.Is(err, errNoLongerCollectable):
	case blobref.IsForeignKeyRestrict(err):
		slog.Debug("cleanup: "+what+" still referenced from another layer; skipped", "id", id)
	default:
		slog.Warn("cleanup: reclaim "+what+" failed", "id", id, "error", err)
	}
}

// errNoLongerCollectable rolls back a reclaim whose object stopped matching
// the predicate that listed it.
var errNoLongerCollectable = errors.New("cleanup: object no longer collectable")

// sweepUnconfirmedObjects reclaims reservations whose upload never arrived, in
// any layer: the reservations this product owns go with the object in one
// transaction, and the object's key and their upload keys are queued for
// removal.
//
// The cutoff is at least the 45 minutes PROTOCOL.md section Storage sets, so
// an upload URL another product handed out for the row has expired.
func sweepUnconfirmedObjects(ctx context.Context, db DB, q *generated.Queries, storageClient *storage.Client, olderThan time.Time) {
	if storageClient == nil {
		return
	}
	list := func(afterID uint32) ([]candidate, error) {
		rows, err := q.ListUnconfirmedStorageObjects(ctx, generated.ListUnconfirmedStorageObjectsParams{
			Cutoff:  sql.NullTime{Time: olderThan, Valid: true},
			AfterID: afterID,
			Limit:   storageSweepBatchSize,
		})
		out := make([]candidate, len(rows))
		for i, r := range rows {
			out[i] = candidate{id: r.ID, key: r.StorageKey}
		}
		return out, err
	}
	pageByID(ctx, "unconfirmed storage objects", list, func(obj candidate) {
		logReclaim("unconfirmed storage object", obj.id, dbtx.Run(ctx, db, func(tq *generated.Queries) error {
			reservations, err := tq.ListReservationsOfObject(ctx, obj.id)
			if err != nil {
				return err
			}
			if err := tq.DeleteReservationsOfObject(ctx, obj.id); err != nil {
				return err
			}
			res, err := tq.DeleteUnconfirmedStorageObject(ctx, obj.id)
			if err != nil {
				return err
			}
			// A confirm landed since the listing: roll the reservations back.
			if deleted, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if !deleted {
				return errNoLongerCollectable
			}
			keys := []string{obj.key}
			for _, r := range reservations {
				keys = append(keys, attachmentUploadKey(r.WorkspacePublicID, r.PublicID))
			}
			return blobref.QueueRemoval(ctx, tq, keys...)
		}))
	})
}

// sweepUnreferencedObjects removes confirmed objects nothing points at any
// more, and queues their blobs. No age condition is needed: the view lists
// confirmed objects only, so a reservation in flight is never taken here.
//
// The delete re-checks through the view, so an object that gained a referrer
// since it was listed is left alone.
func sweepUnreferencedObjects(ctx context.Context, db DB, q *generated.Queries, storageClient *storage.Client) {
	if storageClient == nil {
		return
	}
	list := func(afterID uint32) ([]candidate, error) {
		rows, err := q.ListUnreferencedStorageObjects(ctx, generated.ListUnreferencedStorageObjectsParams{
			ID:    afterID,
			Limit: storageSweepBatchSize,
		})
		out := make([]candidate, len(rows))
		for i, r := range rows {
			out[i] = candidate{id: r.ID, key: r.StorageKey}
		}
		return out, err
	}
	pageByID(ctx, "unreferenced storage objects", list, func(obj candidate) {
		logReclaim("unreferenced storage object", obj.id, dbtx.Run(ctx, db, func(tq *generated.Queries) error {
			res, err := tq.DeleteStorageObject(ctx, obj.id)
			if err != nil {
				return err
			}
			if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
				return err
			}
			return blobref.QueueRemoval(ctx, tq, obj.key)
		}))
	})
}

// drainBlobRemovals removes the blobs queued in storage_blob_removals: the
// blob first, the queue row second, so a failed removal is retried on the
// next pass. Removing a key that is already gone is not an error. A key a
// confirmed object claims again since it was queued is dropped without
// touching the blob.
//
// Both products drain the same queue, which is safe only while they write to
// the same bucket; Run checks that before letting this run.
func drainBlobRemovals(ctx context.Context, q *generated.Queries, storageClient *storage.Client) {
	if storageClient == nil {
		return
	}
	var cursor uint32
	for range maxBatches {
		rows, err := q.ListStorageBlobRemovals(ctx, generated.ListStorageBlobRemovalsParams{
			ID:    cursor,
			Limit: storageSweepBatchSize,
		})
		if err != nil {
			slog.Warn("cleanup: list queued blob removals failed", "error", err)
			return
		}
		for _, row := range rows {
			cursor = row.ID
			claimed, err := q.CountConfirmedStorageObjectsByKey(ctx, row.StorageKey)
			if err != nil {
				slog.Warn("cleanup: count objects for queued key failed", "key", row.StorageKey, "error", err)
				continue
			}
			if claimed == 0 {
				if err := storageClient.DeleteObject(ctx, row.StorageKey); err != nil {
					slog.Warn("cleanup: queued blob removal failed; kept for retry", "key", row.StorageKey, "error", err)
					continue
				}
			}
			if err := q.DeleteStorageBlobRemoval(ctx, row.ID); err != nil {
				slog.Warn("cleanup: drop queued blob removal failed", "id", row.ID, "error", err)
			}
		}
		if len(rows) < storageSweepBatchSize {
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
