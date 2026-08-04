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

// parseSHA256 accepts the digest the client computed over the bytes it is
// about to upload; storage_objects is keyed on it.
func parseSHA256(s string) ([]byte, bool) {
	if len(s) != 64 {
		return nil, false
	}
	raw, err := hex.DecodeString(strings.ToLower(s))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// PresignAvatar issues a presigned PUT URL for uploading the current user's avatar.
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

		digest, ok := parseSHA256(in.Body.SHA256)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
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

		url, err := deps.Storage.PresignPut(ctx, key, in.Body.ContentType, in.Body.ByteSize, avatarUploadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
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
		if uint64(info.Size) != upload.ByteSize || info.Size > maxAvatarSize || actualContentType != upload.ContentType {
			if err := deps.Storage.DeleteObject(ctx, upload.StorageKey); err != nil {
				slog.WarnContext(ctx, "failed to delete invalid avatar upload", "key", upload.StorageKey, "error", err)
				return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
			}
			if err := deps.Queries.DeleteAvatarUpload(ctx, upload.ID); err != nil {
				slog.WarnContext(ctx, "failed to delete invalid avatar upload session", "uploadID", upload.ID, "error", err)
			}
			return nil, apierrors.ToHuma(apierrors.AvatarUploadInvalid)
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
		objectPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if _, err := q.CreateStorageObject(ctx, generated.CreateStorageObjectParams{
			PublicID:    objectPubID[:],
			OwnerUserID: sql.NullInt32{Int32: int32(userID), Valid: true},
			Sha256:      upload.Sha256,
			ByteSize:    upload.ByteSize,
			ContentType: upload.ContentType,
			StorageKey:  upload.StorageKey,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		object, err := q.GetStorageObjectByKey(ctx, upload.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		previousObjectID := user.AvatarStorageObjectID

		if err := q.SetUserAvatarObject(ctx, generated.SetUserAvatarObjectParams{
			AvatarStorageObjectID: sql.NullInt32{Int32: int32(object.ID), Valid: true},
			ID:                    userID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := q.IncrementStorageObjectRefs(ctx, object.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if previousObjectID.Valid && uint32(previousObjectID.Int32) != object.ID {
			if err := q.DecrementStorageObjectRefs(ctx, uint32(previousObjectID.Int32)); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

// DeleteAvatar clears the user's avatar and removes the stored object.
func DeleteAvatar(deps Deps) func(context.Context, *DeleteAvatarInput) (*DeleteAvatarOutput, error) {
	return func(ctx context.Context, _ *DeleteAvatarInput) (*DeleteAvatarOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Clearing the avatar releases the reference; the blob itself is left
		// to the sweep, because the same picture may still be somebody's.
		if user.AvatarStorageObjectID.Valid {
			objectID := uint32(user.AvatarStorageObjectID.Int32)
			if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
				if err := q.ClearUserAvatar(ctx, userID); err != nil {
					return err
				}
				return q.DecrementStorageObjectRefs(ctx, objectID)
			}); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
		} else if user.AvatarURL.Valid {
			if err := deps.Queries.ClearUserAvatar(ctx, userID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
		}

		refreshed, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteAvatarOutput{Body: mapUserWithAvatar(ctx, deps, refreshed)}, nil
	}
}
