// Package albumblob moves an album photo onto the object model the rest of
// the product's blobs use.
//
// It exists as its own package because two callers have to do it identically:
// the handler that confirms a new upload, and the sweep that backfills photos
// taken before the album had a storage_object_id. The rule that decides
// whether a reference is taken lives here so the two cannot drift -- a second
// copy that took a reference on a row already attached would pin the bytes
// forever.
package albumblob

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

func nullInt32(v uint32) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(v), Valid: true}
}

// ThumbnailKey puts a photo's grid-sized rendering underneath the photo's own
// key, so it is derivable from the photo alone.
//
// It lives here rather than beside the handler that signs the upload because
// the sweep needs it too, and for the case where nothing else can supply it: a
// thumbnail whose upload was never confirmed reached no storage object, so
// after the photo's row is deleted the only thing that could still have named
// those bytes is this rule.
func ThumbnailKey(photoKey string) string {
	return photoKey + "/thumb"
}

// Photo is what attaching needs to know about the row being moved. It is
// spelled out rather than taking generated.AlbumPhoto because the backfill
// sweep reads only these columns.
//
// ID and WorkspaceID name the row. The blob fields describe whichever rendering
// is being attached: the picture itself for Attach, the smaller one for
// AttachThumbnail.
type Photo struct {
	ID          uint32
	WorkspaceID uint32
	StorageKey  string
	ContentType string
	ByteSize    uint64
	// SHA256 is the digest of the bytes actually in storage, which only a read
	// of the object can say. Nothing here computes it: the read is I/O and
	// belongs outside whatever transaction the caller is in.
	SHA256 []byte
}

// Attach finds or creates the storage object for a photo's bytes and points
// the photo at it, taking a reference if and only if this call is the one
// that attached the row.
//
// The object carries the key the bytes are actually at, which for now is the
// photo's own key rather than a content-addressed one: the client PUTs before
// anything knows the digest, so the bytes cannot be written at a key derived
// from it. Two photos with identical bytes therefore share one row -- and one
// row's key -- while each keeps its own copy of the bytes. That is a shared
// reference, not shared storage; reclaiming the duplicate belongs to the
// change that drops album_photos.storage_key and moves the blobs.
//
// q may be transaction-bound: the caller decides what else lands with this.
func Attach(ctx context.Context, q *generated.Queries, photo Photo) error {
	object, err := ensureObject(ctx, q, photo)
	if err != nil {
		return err
	}
	res, err := q.AttachAlbumPhotoStorageObject(ctx, generated.AttachAlbumPhotoStorageObjectParams{
		StorageObjectID: nullInt32(object.ID),
		ID:              photo.ID,
	})
	if err != nil {
		return err
	}
	return referenceIfAttached(ctx, q, res, object.ID)
}

// AttachThumbnail does the same for a photo's grid-sized rendering, which is a
// second object the same photo holds -- the same reference count, the same
// sweep, and released by the same delete.
//
// It is a separate call rather than an argument to Attach because the two
// happen at different moments and must fail separately: a thumbnail is optional
// and arrives (or does not) after the picture is already the photo's.
func AttachThumbnail(ctx context.Context, q *generated.Queries, thumbnail Photo) error {
	object, err := ensureObject(ctx, q, thumbnail)
	if err != nil {
		return err
	}
	res, err := q.AttachAlbumPhotoThumbnailObject(ctx, generated.AttachAlbumPhotoThumbnailObjectParams{
		ThumbnailObjectID: nullInt32(object.ID),
		ID:                thumbnail.ID,
	})
	if err != nil {
		return err
	}
	return referenceIfAttached(ctx, q, res, object.ID)
}

// referenceIfAttached increments the object's count only when this call is the
// one that wrote the column.
//
// The updates re-check the column IS NULL, so a confirm and a backfill pass
// racing over one photo produce one attachment and one reference. Incrementing
// without that check is how a blob acquires a count nothing will ever return.
func referenceIfAttached(ctx context.Context, q *generated.Queries, res sql.Result, objectID uint32) error {
	attached, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if attached != 1 {
		return nil
	}
	return q.IncrementStorageObjectRefs(ctx, objectID)
}

// ensureObject finds or creates the storage object for a blob's bytes.
func ensureObject(ctx context.Context, q *generated.Queries, photo Photo) (generated.StorageObject, error) {
	objectPubID, err := uuid.NewV7()
	if err != nil {
		return generated.StorageObject{}, err
	}
	// Content-addressed within the workspace, never the uploader: the
	// user-scoped foreign key is ON DELETE CASCADE, so an object owned by the
	// person who uploaded it would be deleted along with their account -- out
	// from under a photo the calendar still shows, before the RESTRICT on the
	// photo's own reference could refuse it.
	if _, err := q.CreateStorageObject(ctx, generated.CreateStorageObjectParams{
		PublicID:    objectPubID[:],
		WorkspaceID: nullInt32(photo.WorkspaceID),
		Sha256:      photo.SHA256,
		ByteSize:    photo.ByteSize,
		ContentType: photo.ContentType,
		StorageKey:  photo.StorageKey,
	}); err != nil {
		return generated.StorageObject{}, err
	}
	// Read the row back by digest rather than by key or by LastInsertId. On
	// the dedup path the upsert inserted nothing, and the row that was already
	// there carries the key of whichever photo stored these bytes first --
	// looking it up by this photo's own key would find nothing at all.
	return q.GetStorageObjectByWorkspaceDigest(ctx, generated.GetStorageObjectByWorkspaceDigestParams{
		WorkspaceID: nullInt32(photo.WorkspaceID),
		Sha256:      photo.SHA256,
	})
}
