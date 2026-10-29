// Package blobref owns the storage_objects rows every blob in the product goes
// through: finding the object identical bytes deduplicate onto, recording that
// its bytes were verified, and taking and giving back references to it.
//
// Attachments, album photos and avatars all refer to objects, and each used to
// do this itself. The rules of sql/core/PROTOCOL.md section Storage have to
// hold for all of them at once, which is why they live here and nowhere else:
//
//   - An object whose bytes were never read back and checked against its digest
//     is a reservation, not something to reuse. Only MarkVerified marks one,
//     and only a verified object is offered as already stored.
//   - ref_count is the number of rows pointing at the object, in any state:
//     reserved, live or retired. The transaction that inserts or sets such a
//     row takes the reference, the one that deletes or clears it gives it
//     back; a soft delete changes nothing. Acquire and Release are the only
//     writes to the count.
//   - storage_key is fixed when the object is created. Confirming an upload
//     never moves the object to other bytes.
//   - Blobs are removed only through storage_blob_removals: QueueRemoval in
//     the transaction that deleted the rows, then the cleanup drainer -- or,
//     for keys a request has just orphaned, RemoveQueued right after commit,
//     leaving whatever fails to the drainer.
package blobref

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// ErrUnverifiedElsewhere is MarkVerified refusing bytes checked at a key other
// than the unverified object's own. The object's key cannot be rewritten
// (another product may have handed out an upload URL for it), so the bytes
// cannot become the object; the caller leaves its row unattached.
var ErrUnverifiedElsewhere = errors.New("blobref: unverified object lives at another key")

// Blob describes bytes by the scope they are deduplicated within and their
// digest. Exactly one of WorkspaceID and OwnerUserID is set, matching the
// storage_objects scope constraint.
type Blob struct {
	WorkspaceID uint32
	OwnerUserID uint32
	SHA256      []byte
	ByteSize    uint64
	ContentType string
	// StorageKey is where these bytes are, or are about to be, written. An
	// object that already exists for the digest keeps its own key.
	StorageKey string
}

// Verified reports whether an object's bytes were read back and matched its
// digest. An unverified object holds a size and type somebody declared and may
// have no bytes behind it at all.
func Verified(object generated.StorageObject) bool {
	return object.UploadedAt.Valid
}

// Reserve finds or creates the object for b's digest without vouching for any
// bytes, and takes the reference for the row the caller inserts pointing at it
// in the same transaction. It returns the existing row when there is one,
// verified or not -- the unique key on (scope, sha256) leaves no room for a
// second -- so the caller must check Verified before treating the bytes as
// present.
//
// Taking the reference also restamps updated_at, which is the clock the
// unconfirmed sweep ages a reservation by: each upload URL handed out for the
// row pushes its reclaim back.
//
// q must be transaction-bound: the row is read FOR UPDATE, and the reference
// is only right if the caller's insert commits with it.
func Reserve(ctx context.Context, q *generated.Queries, b Blob) (generated.StorageObject, error) {
	object, err := findOrCreate(ctx, q, b)
	if err != nil {
		return generated.StorageObject{}, err
	}
	if err := Acquire(ctx, q, object.ID); err != nil {
		return generated.StorageObject{}, err
	}
	object.RefCount++
	return object, nil
}

// findOrCreate is Reserve without the reference, for callers whose row points
// at the object only once its bytes are verified.
func findOrCreate(ctx context.Context, q *generated.Queries, b Blob) (generated.StorageObject, error) {
	objectPubID, err := uuid.NewV7()
	if err != nil {
		return generated.StorageObject{}, err
	}
	if _, err := q.CreateStorageObject(ctx, generated.CreateStorageObjectParams{
		PublicID:    objectPubID[:],
		WorkspaceID: nullID(b.WorkspaceID),
		OwnerUserID: nullID(b.OwnerUserID),
		Sha256:      b.SHA256,
		ByteSize:    b.ByteSize,
		ContentType: b.ContentType,
		StorageKey:  b.StorageKey,
	}); err != nil {
		return generated.StorageObject{}, err
	}
	// Read back by digest rather than by key or LastInsertId: on the dedup path
	// the upsert inserted nothing, and the row that was there may carry a key
	// other than b's.
	if b.OwnerUserID != 0 {
		return q.GetStorageObjectByOwnerDigest(ctx, generated.GetStorageObjectByOwnerDigestParams{
			OwnerUserID: nullID(b.OwnerUserID),
			Sha256:      b.SHA256,
		})
	}
	return q.GetStorageObjectByWorkspaceDigest(ctx, generated.GetStorageObjectByWorkspaceDigestParams{
		WorkspaceID: nullID(b.WorkspaceID),
		Sha256:      b.SHA256,
	})
}

// Store finds or creates the object for bytes the caller has just read back at
// b.StorageKey and found to match b.SHA256, and marks it verified. It takes no
// reference: the caller does, when it points its row at the object.
//
// An already verified object is returned as it is. An unverified one at
// b.StorageKey is marked verified; one at another key -- a reservation still
// waiting for its own upload -- is refused with ErrUnverifiedElsewhere.
//
// q must be transaction-bound.
func Store(ctx context.Context, q *generated.Queries, b Blob) (generated.StorageObject, error) {
	object, err := findOrCreate(ctx, q, b)
	if err != nil {
		return generated.StorageObject{}, err
	}
	return MarkVerified(ctx, q, object, b)
}

// MarkVerified records that b's bytes, at b.StorageKey, were checked against
// object's digest, and returns the row as it now stands. A row verified before
// is left alone. Bytes checked anywhere but the object's own key are refused
// with ErrUnverifiedElsewhere.
//
// q must be transaction-bound, and object must have been read FOR UPDATE in it.
func MarkVerified(ctx context.Context, q *generated.Queries, object generated.StorageObject, b Blob) (generated.StorageObject, error) {
	if Verified(object) {
		return object, nil
	}
	if object.StorageKey != b.StorageKey {
		return generated.StorageObject{}, ErrUnverifiedElsewhere
	}
	if err := q.MarkStorageObjectUploaded(ctx, generated.MarkStorageObjectUploadedParams{
		ByteSize:    b.ByteSize,
		ContentType: b.ContentType,
		ID:          object.ID,
	}); err != nil {
		return generated.StorageObject{}, err
	}
	return q.GetStorageObjectByID(ctx, object.ID)
}

// Acquire takes one reference on an object, for a row the caller's
// transaction has just inserted or pointed at it.
func Acquire(ctx context.Context, q *generated.Queries, objectID uint32) error {
	return q.IncrementStorageObjectRefs(ctx, objectID)
}

// Release gives back one reference, for a row the caller's transaction has
// just deleted or cleared.
func Release(ctx context.Context, q *generated.Queries, objectID uint32) error {
	return q.DecrementStorageObjectRefs(ctx, objectID)
}

// QueueRemoval records keys whose blobs are to be removed. It belongs in the
// transaction that deleted the rows naming them, so the record commits with
// the delete; the cleanup drainer removes the blobs afterwards.
func QueueRemoval(ctx context.Context, q *generated.Queries, keys ...string) error {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := q.QueueStorageBlobRemoval(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Remover is the slice of the storage client RemoveQueued needs.
type Remover interface {
	DeleteObject(ctx context.Context, key string) error
	Location() string
}

// ErrLocationMismatch is the database recording another bucket than the one
// configured. Removing blobs then deletes keys in the wrong bucket while the
// blobs the queue names leak.
var ErrLocationMismatch = errors.New("blobref: configured storage location differs from the one recorded for this database")

// CheckLocation compares location with instance_settings 'storage.location',
// recording it when the database has none (PROTOCOL.md section Storage).
func CheckLocation(ctx context.Context, q *generated.Queries, location string) error {
	pub, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err := q.InsertStorageLocationIfAbsent(ctx, generated.InsertStorageLocationIfAbsentParams{
		PublicID: pub[:],
		Location: location,
	}); err != nil {
		return fmt.Errorf("blobref: record storage location: %w", err)
	}
	return compareLocation(ctx, q, location)
}

func compareLocation(ctx context.Context, q *generated.Queries, location string) error {
	recorded, err := q.GetStorageLocation(ctx)
	if err != nil {
		return fmt.Errorf("blobref: read storage location: %w", err)
	}
	if recorded != location {
		return fmt.Errorf("%w: configured %q, recorded %q", ErrLocationMismatch, location, recorded)
	}
	return nil
}

// locationGate is the process's one answer to whether this bucket may have
// blobs removed from it, shared by the drainer and the request paths so
// neither removes what the other has been told not to.
var locationGate struct {
	sync.Mutex
	decided bool
	err     error
}

// LocationAllowsRemoval runs the drainer's check the first time it is asked
// and caches the outcome for the life of the process.
func LocationAllowsRemoval(ctx context.Context, q *generated.Queries, location string) error {
	locationGate.Lock()
	defer locationGate.Unlock()
	if locationGate.decided {
		return locationGate.err
	}
	err := CheckLocation(ctx, q, location)
	if err == nil || errors.Is(err, ErrLocationMismatch) {
		locationGate.decided, locationGate.err = true, err
	}
	return err
}

// errLocationUndecided is a request path finding no bucket recorded yet. It
// does not record one: that is the drainer's first act, and a request must not
// claim the database for whatever bucket it happens to be configured with.
var errLocationUndecided = errors.New("blobref: storage location not yet recorded")

// requestAllowsRemoval is the gate as a request path reads it: the cached
// outcome when there is one, else a comparison that records nothing and is
// cached only once it has an answer.
func requestAllowsRemoval(ctx context.Context, q *generated.Queries, location string) error {
	locationGate.Lock()
	defer locationGate.Unlock()
	if locationGate.decided {
		return locationGate.err
	}
	err := compareLocation(ctx, q, location)
	if errors.Is(err, sql.ErrNoRows) {
		return errLocationUndecided
	}
	if err == nil || errors.Is(err, ErrLocationMismatch) {
		locationGate.decided, locationGate.err = true, err
	}
	return err
}

// RemoveQueued removes the blobs at keys the caller queued in a transaction
// that has now committed, and drops their queue rows, so a request that
// orphaned bytes does not leave them in the way until the next cleanup tick.
// The checks are the drainer's: nothing is removed until the bucket matches
// the recorded location (left queued instead), and a key a confirmed object
// claims is not removed. Anything that fails stays queued for the drainer.
// Best-effort by design; failures are returned joined for the caller to log.
func RemoveQueued(ctx context.Context, q *generated.Queries, store Remover, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := requestAllowsRemoval(ctx, q, store.Location()); err != nil {
		if errors.Is(err, errLocationUndecided) {
			return nil
		}
		return err
	}
	var errs []error
	for _, key := range keys {
		if key == "" {
			continue
		}
		claimed, err := q.CountConfirmedStorageObjectsByKey(ctx, key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if claimed == 0 {
			if err := store.DeleteObject(ctx, key); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := q.DeleteStorageBlobRemovalByKey(ctx, key); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// IsForeignKeyRestrict reports whether err is a delete refused because a
// RESTRICT foreign key still references the row. For a storage object that
// means a layer this product does not own still holds it.
func IsForeignKeyRestrict(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == mysqlRowIsReferenced2 || me.Number == mysqlRowIsReferenced)
}

const (
	mysqlRowIsReferenced  uint16 = 1217 // ER_ROW_IS_REFERENCED
	mysqlRowIsReferenced2 uint16 = 1451 // ER_ROW_IS_REFERENCED_2
)

// ChangedOne reports whether a guarded update changed exactly one row, which
// is what entitles its transaction to Acquire or Release for it.
func ChangedOne(res sql.Result) (bool, error) {
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func nullID(v uint32) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(v), Valid: v != 0}
}
