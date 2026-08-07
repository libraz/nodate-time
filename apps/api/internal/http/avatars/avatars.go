// Package avatars resolves a user's picture to the single URL a response
// carries.
//
// A picture is stored one of two ways: an object the user uploaded, which the
// user row names by id, or an external URL carried over from the identity
// provider they signed in with. An uploaded object only becomes a URL once it
// is signed, so choosing between the two -- and the signing -- lives here
// rather than in every handler that renders a person.
package avatars

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// TTL is how long a signed avatar URL stays valid. Avatars are embedded in
// listings a reader keeps open -- a member sheet, a comment thread, an
// activity feed -- and consumed over however long they keep it open, so a
// short life shows up as pictures that break while the page is still on
// screen. An hour outlasts a session, and the client re-lists when one expires.
const TTL = time.Hour

// Resolver answers with one URL per user for the span of one request.
//
// Signatures are memoized by storage key, so a thread of fifty comments by
// three people signs three URLs and every row by the same person carries the
// same one.
//
// It holds per-request state and is not safe for concurrent use.
type Resolver struct {
	queries *generated.Queries
	storage *storage.Client
	signed  map[string]string
}

// New returns a resolver for one request. A nil storage client resolves every
// avatar to its external URL, which is what a deployment without object
// storage has.
func New(q *generated.Queries, s *storage.Client) *Resolver {
	return &Resolver{queries: q, storage: s}
}

// FromKey resolves a row that already carries the uploaded object's storage
// key. This is the form a listing uses: the key is joined in alongside the
// row, so a whole page costs no queries of its own.
func (r *Resolver) FromKey(ctx context.Context, storageKey, externalURL sql.NullString) string {
	if url := r.sign(ctx, storageKey.String, storageKey.Valid); url != "" {
		return url
	}
	return externalURL.String
}

// FromObjectID resolves a row that names the object by its internal id, which
// costs one read to find the key.
//
// For single-row responses. A listing must join the key in and use FromKey, or
// it pays that read per row.
func (r *Resolver) FromObjectID(ctx context.Context, objectID sql.NullInt32, externalURL sql.NullString) string {
	if r.storage != nil && r.queries != nil && objectID.Valid {
		if obj, err := r.queries.GetStorageObjectByID(ctx, uint32(objectID.Int32)); err == nil {
			if url := r.sign(ctx, obj.StorageKey, true); url != "" {
				return url
			}
		}
	}
	return externalURL.String
}

// ForUser resolves a whole user row.
func (r *Resolver) ForUser(ctx context.Context, u generated.User) string {
	return r.FromObjectID(ctx, u.AvatarStorageObjectID, u.AvatarURL)
}

// sign presigns a storage key. It answers "" both when there is nothing to
// sign and when signing fails, because either way the caller should fall back
// to the external URL rather than hand back one that cannot be fetched.
func (r *Resolver) sign(ctx context.Context, storageKey string, present bool) string {
	if r.storage == nil || !present || storageKey == "" {
		return ""
	}
	if url, ok := r.signed[storageKey]; ok {
		return url
	}
	url, err := r.storage.PresignGet(ctx, storageKey, TTL)
	if err != nil {
		slog.WarnContext(ctx, "failed to presign avatar URL", "key", storageKey, "error", err)
		return ""
	}
	if r.signed == nil {
		r.signed = make(map[string]string)
	}
	r.signed[storageKey] = url
	return url
}
