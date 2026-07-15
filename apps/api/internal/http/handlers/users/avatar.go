package users

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
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

func avatarStorageKey(userPubHex, avatarPubHex string) string {
	return fmt.Sprintf("%s/%s/%s", avatarStoragePath, userPubHex, avatarPubHex)
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

		avatarPubID, _ := uuid.NewV7()
		userPubHex := pubIDToHex(user.PublicID)
		avatarPubHex := avatarPubID.String()
		key := avatarStorageKey(userPubHex, avatarPubHex)
		expiresAt := time.Now().Add(avatarUploadTTL)

		activeUploads, err := deps.Queries.CountActiveAvatarUploads(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if activeUploads >= maxAvatarUploads {
			return nil, apierrors.ToHuma(apierrors.AvatarUploadLimit)
		}

		url, err := deps.Storage.PresignPut(ctx, key, in.Body.ContentType, avatarUploadTTL)
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
			StorageKey:  key,
			ContentType: strings.ToLower(in.Body.ContentType),
			ByteSize:    in.Body.ByteSize,
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
		out.Body.StorageKey = key
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
		if info.Size != upload.ByteSize || info.Size > maxAvatarSize || actualContentType != upload.ContentType {
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

		oldKey := ""
		if user.AvatarStorageKey.Valid {
			oldKey = user.AvatarStorageKey.String
		}

		err = q.UpdateUserAvatar(ctx, generated.UpdateUserAvatarParams{
			AvatarStorageKey:  sql.NullString{String: upload.StorageKey, Valid: true},
			AvatarContentType: sql.NullString{String: upload.ContentType, Valid: true},
			ID:                userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := q.DeleteAvatarUpload(ctx, upload.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if oldKey != "" && oldKey != upload.StorageKey {
			if err := deps.Storage.DeleteObject(ctx, oldKey); err != nil {
				slog.WarnContext(ctx, "failed to delete previous avatar", "key", oldKey, "error", err)
			}
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

		if user.AvatarStorageKey.Valid && user.AvatarStorageKey.String != "" {
			if deps.Storage != nil {
				if err := deps.Storage.DeleteObject(ctx, user.AvatarStorageKey.String); err != nil {
					slog.WarnContext(ctx, "failed to delete avatar object", "key", user.AvatarStorageKey.String, "error", err)
				}
			}
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
