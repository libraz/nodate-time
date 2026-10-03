// Package blobref owns the storage_objects rows every blob in the product goes
// through: finding the object identical bytes deduplicate onto, recording that
// its bytes were verified, and taking and giving back references to it.
//
// Attachments, album photos and avatars all refer to objects, and each used to
// do this itself. Two rules have to hold for all of them at once, which is why
// they live here and nowhere else:
//
//   - An object whose bytes were never read back and checked against its digest
//     is a reservation, not something to reuse. Only Store marks one verified,
//     and only a verified object is offered as already stored.
//   - A reference is taken by the transaction that attached a row and given
//     back by the one that detached it. Callers decide that from rows they
//     actually changed; Acquire and Release are the only writes to the count.
package blobref

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

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
// bytes. It returns the existing row when there is one, verified or not --
// the unique key on (scope, sha256) leaves no room for a second -- so the
// caller must check Verified before treating the bytes as present.
//
// q must be transaction-bound: the row is read FOR UPDATE.
func Reserve(ctx context.Context, q *generated.Queries, b Blob) (generated.StorageObject, error) {
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
// b.StorageKey and found to match b.SHA256, and marks it verified.
//
// An already verified object is returned as it is. An unverified one -- a
// reservation whose upload has not been confirmed -- is taken over: it now
// points at b's key, size and type, because those are the bytes somebody has
// actually checked.
//
// q must be transaction-bound.
func Store(ctx context.Context, q *generated.Queries, b Blob) (generated.StorageObject, error) {
	object, err := Reserve(ctx, q, b)
	if err != nil {
		return generated.StorageObject{}, err
	}
	return MarkVerified(ctx, q, object, b)
}

// MarkVerified records that b's bytes, at b.StorageKey, were checked against
// object's digest, and returns the row as it now stands. A row verified before
// is left alone.
//
// q must be transaction-bound, and object must have been read FOR UPDATE in it.
func MarkVerified(ctx context.Context, q *generated.Queries, object generated.StorageObject, b Blob) (generated.StorageObject, error) {
	if Verified(object) {
		return object, nil
	}
	if err := q.MarkStorageObjectUploaded(ctx, generated.MarkStorageObjectUploadedParams{
		StorageKey:  b.StorageKey,
		ByteSize:    b.ByteSize,
		ContentType: b.ContentType,
		ID:          object.ID,
	}); err != nil {
		return generated.StorageObject{}, err
	}
	return q.GetStorageObjectByID(ctx, object.ID)
}

// Acquire takes one reference on an object, for a row the caller's
// transaction has just attached to it.
func Acquire(ctx context.Context, q *generated.Queries, objectID uint32) error {
	return q.IncrementStorageObjectRefs(ctx, objectID)
}

// Release gives back one reference, for a row the caller's transaction has
// just detached -- or, when the rows are not changed one at a time, has read
// FOR UPDATE and is retiring in the same transaction.
func Release(ctx context.Context, q *generated.Queries, objectID uint32) error {
	return q.DecrementStorageObjectRefs(ctx, objectID)
}

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
