package users

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

const (
	maxAvatarSize      = 5 * 1024 * 1024
	maxAvatarUploads   = 5
	avatarUploadTTL    = 15 * time.Minute
	avatarStoragePath  = "avatars"
	avatarContentTypes = "image/jpeg,image/png,image/webp"
)

func isAcceptedImageContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	for _, allowed := range strings.Split(avatarContentTypes, ",") {
		if ct == allowed {
			return true
		}
	}
	return false
}

// avatarStorageKey is built from the user and the digest of the bytes, so
// the same picture uploaded twice lands on one object rather than two.
func avatarStorageKey(userPubHex, sha256Hex string) string {
	return fmt.Sprintf("%s/%s/%s", avatarStoragePath, userPubHex, sha256Hex)
}

// PresignAvatar issues a signed PUT URL for uploading the current user's avatar.
// The caller must then call ConfirmAvatar with the returned avatarId to finalize
// the change.
func PresignAvatar(deps Deps) func(context.Context, *PresignAvatarInput) (*PresignAvatarOutput, error) {
	return func(ctx context.Context, in *PresignAvatarInput) (*PresignAvatarOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if in.Body.ByteSize > maxAvatarSize {
			return nil, apierrors.ToHuma(apierrors.AvatarTooLarge)
		}
		if !isAcceptedImageContentType(in.Body.ContentType) {
			return nil, apierrors.ToHuma(apierrors.InvalidImageContentType)
		}

		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		digest, err := blobref.ParseDigest(in.Body.SHA256)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.UploadDigestMismatch)
		}

		avatarPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		userPubHex := pubIDToHex(user.PublicID)
		avatarPubHex := avatarPubID.String()
		key := avatarStorageKey(userPubHex, hex.EncodeToString(digest))
		expiresAt := time.Now().Add(avatarUploadTTL)

		activeUploads, err := deps.Queries.CountActiveAvatarUploads(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if activeUploads >= maxAvatarUploads {
			return nil, apierrors.ToHuma(apierrors.AvatarUploadLimit)
		}

		// The same picture uploaded again has the same key, and the relay never
		// overwrites a stored key, so the bytes already there are confirmed
		// rather than uploaded a second time. ConfirmAvatar checks them against
		// the digest either way.
		_, alreadyStored, err := deps.Storage.StatObject(ctx, key)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		url := ""
		if !alreadyStored {
			url, err = deps.Storage.SignedPutURL(key, in.Body.ContentType, in.Body.ByteSize, avatarUploadTTL)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
			}
		}
		if deps.DB == nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		q := generated.New(tx)
		if _, err := q.GetUserByIDForUpdate(ctx, userID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		activeUploads, err = q.CountActiveAvatarUploads(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if activeUploads >= maxAvatarUploads {
			return nil, apierrors.ToHuma(apierrors.AvatarUploadLimit)
		}
		if _, err := q.CreateAvatarUpload(ctx, generated.CreateAvatarUploadParams{
			PublicID:    avatarPubID[:],
			UserID:      userID,
			Sha256:      digest,
			StorageKey:  key,
			ContentType: strings.ToLower(in.Body.ContentType),
			ByteSize:    uint64(in.Body.ByteSize),
			ExpiresAt:   expiresAt,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &PresignAvatarOutput{}
		out.Body.AvatarID = avatarPubHex
		out.Body.UploadURL = url
		return out, nil
	}
}

// ConfirmAvatar finalizes an uploaded avatar by writing its storage key to the
// users table. If the user previously had an avatar, the old object is removed
// best-effort.
func ConfirmAvatar(deps Deps) func(context.Context, *ConfirmAvatarInput) (*ConfirmAvatarOutput, error) {
	return func(ctx context.Context, in *ConfirmAvatarInput) (*ConfirmAvatarOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		avatarPubID, err := uuid.Parse(in.Body.AvatarID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AvatarNotFound)
		}

		upload, err := deps.Queries.GetAvatarUploadForUser(ctx, generated.GetAvatarUploadForUserParams{
			PublicID: avatarPubID[:],
			UserID:   userID,
		})
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, apierrors.ToHuma(apierrors.AvatarNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		info, exists, err := deps.Storage.StatObject(ctx, upload.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !exists {
			return nil, apierrors.ToHuma(apierrors.AvatarNotFound)
		}
		actualContentType := strings.ToLower(strings.TrimSpace(info.ContentType))
		valid := uint64(info.Size) == upload.ByteSize && info.Size <= maxAvatarSize && actualContentType == upload.ContentType
		rejection := apierrors.AvatarUploadInvalid
		if valid {
			// The key was derived from the declared digest, so only reading the
			// bytes back says they are the picture the key names.
			if err := verifyStored(ctx, deps, upload); err != nil {
				spec, isVerdict := apierrors.FromBlobref(err)
				if !isVerdict {
					return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
				}
				valid, rejection = false, spec
			}
		}
		if !valid {
			// A key a storage object claims holds verified bytes -- the avatar
			// on display, when the same picture was declared again with another
			// type -- and is not this reservation's to delete.
			used, err := deps.Queries.CountStorageObjectsByKey(ctx, upload.StorageKey)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if used == 0 {
				if err := deps.Storage.DeleteObject(ctx, upload.StorageKey); err != nil {
					slog.WarnContext(ctx, "failed to delete invalid avatar upload", "key", upload.StorageKey, "error", err)
					return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
				}
			}
			if err := deps.Queries.DeleteAvatarUpload(ctx, upload.ID); err != nil {
				slog.WarnContext(ctx, "failed to delete invalid avatar upload session", "uploadID", upload.ID, "error", err)
			}
			return nil, apierrors.ToHuma(rejection)
		}

		if deps.DB == nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		q := generated.New(tx)
		lockedUpload, err := q.GetAvatarUploadForUserForUpdate(ctx, generated.GetAvatarUploadForUserForUpdateParams{
			ID:     upload.ID,
			UserID: userID,
		})
		if err != nil || lockedUpload.StorageKey != upload.StorageKey {
			return nil, apierrors.ToHuma(apierrors.AvatarNotFound)
		}
		user, err := q.GetUserByIDForUpdate(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// The picture is a storage_objects row scoped to this user, and the
		// user row points at it. Releasing the previous reference here rather
		// than deleting its blob is what makes the two-avatars-same-bytes
		// case safe: the object survives while anything still refers to it.
		object, err := blobref.Store(ctx, q, blobref.Blob{
			OwnerUserID: userID,
			SHA256:      upload.Sha256,
			ByteSize:    upload.ByteSize,
			ContentType: upload.ContentType,
			StorageKey:  upload.StorageKey,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// The user row holds one reference, on whichever object it points at.
		// The same picture confirmed again is the same object, and the row
		// already holds its reference.
		previousObjectID := user.AvatarStorageObjectID
		if !previousObjectID.Valid || uint32(previousObjectID.Int32) != object.ID {
			if err := q.SetUserAvatarObject(ctx, generated.SetUserAvatarObjectParams{
				AvatarStorageObjectID: sql.NullInt32{Int32: int32(object.ID), Valid: true},
				ID:                    userID,
			}); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if err := blobref.Acquire(ctx, q, object.ID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if previousObjectID.Valid {
				if err := blobref.Release(ctx, q, uint32(previousObjectID.Int32)); err != nil {
					return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
				}
			}
		}
		if err := q.DeleteAvatarUpload(ctx, upload.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		refreshed, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ConfirmAvatarOutput{Body: mapUserWithAvatar(ctx, deps, refreshed)}
		return out, nil
	}
}

// verifyStored reads the object behind an upload back and checks it against the
// digest declared when the upload was reserved.
func verifyStored(ctx context.Context, deps Deps, upload generated.AvatarUpload) error {
	obj, exists, err := deps.Storage.Open(ctx, upload.StorageKey)
	if err != nil {
		return err
	}
	if !exists {
		return blobref.ErrDigestMismatch
	}
	defer obj.Close()
	return blobref.VerifyReadBack(obj, upload.Sha256, int64(upload.ByteSize))
}

// DeleteAvatar clears the user's avatar and removes the stored object.
func DeleteAvatar(deps Deps) func(context.Context, *DeleteAvatarInput) (*DeleteAvatarOutput, error) {
	return func(ctx context.Context, _ *DeleteAvatarInput) (*DeleteAvatarOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		// Clearing the avatar releases the reference; the blob itself is left
		// to the sweep, because the same picture may still be somebody's. The
		// user row is read under lock so the object released is the one the
		// row held when it was cleared, not one a concurrent confirm replaced.
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			user, err := q.GetUserByIDForUpdate(ctx, userID)
			if err != nil {
				return err
			}
			if !user.AvatarStorageObjectID.Valid && !user.AvatarURL.Valid {
				return nil
			}
			if err := q.ClearUserAvatar(ctx, userID); err != nil {
				return err
			}
			if !user.AvatarStorageObjectID.Valid {
				return nil
			}
			return blobref.Release(ctx, q, uint32(user.AvatarStorageObjectID.Int32))
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteAvatarOutput{}, nil
	}
}
