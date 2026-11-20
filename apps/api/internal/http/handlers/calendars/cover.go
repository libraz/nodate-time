package calendars

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const (
	MaxCalendarCoverSize      = 5 * 1024 * 1024
	maxCalendarCoverDimension = 16384
	maxCalendarCoverPixels    = 40_000_000
	calendarCoverPath         = "calendar-covers"
	calendarCoverImageTTL     = time.Hour
	calendarCoverIOTimeout    = 15 * time.Second
)

// calendarCoverStorageAvailable checks both halves of cover delivery: the
// server can reach storage and it can mint a URL the browser can use to read
// from the relay. No object-store request is made.
func calendarCoverStorageAvailable(deps Deps) bool {
	if deps.Storage == nil {
		return false
	}
	_, err := deps.Storage.SignedGetURL(calendarCoverPath+"/availability-check", calendarCoverImageTTL)
	return err == nil
}

func calendarAllowsUploadedCover(kind generated.CalendarsKind, ownerUserID sql.NullInt32) bool {
	return kind == generated.CalendarsKindPersonal && !ownerUserID.Valid
}

func mapCalendarWithCover(deps Deps, c generated.Calendar, role generated.CalendarMembersRole, memberColor, coverKey string) CalendarResponse {
	response := mapCalendar(c, role, memberColor)
	response.CoverUploadAllowed = calresolve.CanManage(role) && calendarAllowsUploadedCover(c.Kind, c.OwnerUserID) && calendarCoverStorageAvailable(deps)
	if deps.Storage != nil && coverKey != "" {
		if signedURL, err := deps.Storage.SignedGetURL(coverKey, calendarCoverImageTTL); err == nil {
			response.CoverImageURL = signedURL
		}
	}
	return response
}

func calendarCoverKey(ctx context.Context, q *generated.Queries, calendarID uint32) (string, error) {
	rows, err := q.ListCalendarCoverKeys(ctx, []uint32{calendarID})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].StorageKey, nil
}

func calendarCoverStorageKey() (string, error) {
	keyID, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", calendarCoverPath, keyID), nil
}

func imageDimensionsAllowed(width, height int) bool {
	return width > 0 && height > 0 && width <= maxCalendarCoverDimension && height <= maxCalendarCoverDimension &&
		int64(width)*int64(height) <= maxCalendarCoverPixels
}

// sniffCalendarCover validates enough of the image header to establish its
// real format and dimensions. The returned media type comes from bytes rather
// than the browser's Content-Type, which is application/octet-stream.
func sniffCalendarCover(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !imageDimensionsAllowed(config.Width, config.Height) {
		return "", false
	}
	switch format {
	case "jpeg":
		return "image/jpeg", true
	case "png":
		return "image/png", true
	case "webp":
		return "image/webp", true
	default:
		return "", false
	}
}

// PutCalendarCover uploads an image in one bounded request, validates the
// image bytes, verifies storage read-back, and attaches the resulting object
// to the shared calendar.
func PutCalendarCover(deps Deps) func(context.Context, *PutCalendarCoverInput) (*PutCalendarCoverOutput, error) {
	return func(ctx context.Context, in *PutCalendarCoverInput) (*PutCalendarCoverOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		calendar, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if !calendarAllowsUploadedCover(calendar.Kind, calendar.OwnerUserID) {
			return nil, apierrors.ToHuma(apierrors.CalendarCoverForbidden)
		}
		if !calendarCoverStorageAvailable(deps) {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if len(in.RawBody) == 0 {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if len(in.RawBody) > MaxCalendarCoverSize {
			return nil, apierrors.ToHuma(apierrors.CalendarCoverTooLarge)
		}
		contentType, valid := sniffCalendarCover(in.RawBody)
		if !valid {
			return nil, apierrors.ToHuma(apierrors.CalendarCoverInvalid)
		}

		digest := sha256.Sum256(in.RawBody)
		storageKey, err := calendarCoverStorageKey()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		blob := blobref.Blob{
			WorkspaceID: deps.WorkspaceID,
			SHA256:      digest[:],
			ByteSize:    uint64(len(in.RawBody)),
			ContentType: contentType,
			StorageKey:  storageKey,
		}

		// Commit the reservation before touching S3. Its updated_at was
		// refreshed under the digest-row lock by Stage, giving the normal
		// unconfirmed-upload grace period a current timestamp.
		var staged generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, calendar.ID); err != nil {
				return err
			}
			if _, err := calresolve.LockManage(ctx, q, calendar.ID, userID); err != nil {
				return err
			}
			lockedCalendar, err := q.GetCalendarByIDForUpdate(ctx, calendar.ID)
			if err != nil {
				return err
			}
			if !calendarAllowsUploadedCover(lockedCalendar.Kind, lockedCalendar.OwnerUserID) {
				return apierrors.CalendarCoverForbidden
			}
			staged, err = blobref.Stage(ctx, q, blob)
			if err == nil && !blobref.Verified(staged) && !strings.HasPrefix(staged.StorageKey, calendarCoverPath+"/") {
				return apierrors.CalendarCoverConflict
			}
			return err
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		var updated generated.Calendar
		var member generated.CalendarMember
		changed := false
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, calendar.ID); err != nil {
				return err
			}
			var err error
			if member, err = calresolve.LockManage(ctx, q, calendar.ID, userID); err != nil {
				return err
			}
			current, err := q.GetCalendarByIDForUpdate(ctx, calendar.ID)
			if err != nil {
				return err
			}
			if !calendarAllowsUploadedCover(current.Kind, current.OwnerUserID) {
				return apierrors.CalendarCoverForbidden
			}

			oldObjectID, err := q.GetCalendarCoverObjectID(ctx, calendar.ID)
			oldExists := err == nil
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			// Lock both objects in a stable order. A pair of concurrent cover
			// replacements can otherwise each hold the other's old object while
			// waiting for its new one.
			ids := []uint32{staged.ID}
			if oldExists && oldObjectID != staged.ID {
				ids = append(ids, oldObjectID)
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			lockedObjects := make(map[uint32]generated.StorageObject, len(ids))
			for _, objectID := range ids {
				object, err := q.GetStorageObjectByIDForUpdate(ctx, objectID)
				if err != nil {
					return err
				}
				lockedObjects[objectID] = object
			}
			object, ok := lockedObjects[staged.ID]
			if !ok || !object.WorkspaceID.Valid || uint32(object.WorkspaceID.Int32) != deps.WorkspaceID ||
				!bytes.Equal(object.Sha256, digest[:]) || object.StorageKey != staged.StorageKey {
				return apierrors.CalendarCoverConflict
			}
			if !blobref.Verified(object) && !strings.HasPrefix(object.StorageKey, calendarCoverPath+"/") {
				// A pending object from an unrelated feature is not a key this
				// endpoint is allowed to write, even though the digest is shared.
				return apierrors.CalendarCoverConflict
			}

			verifyCtx, cancel := context.WithTimeout(ctx, calendarCoverIOTimeout)
			defer cancel()
			if !blobref.Verified(object) {
				putErr := deps.Storage.Put(verifyCtx, object.StorageKey, bytes.NewReader(in.RawBody), int64(len(in.RawBody)), contentType)
				if putErr != nil && !errors.Is(putErr, storage.ErrObjectExists) {
					return apierrors.StorageUnavailable
				}
			}
			info, exists, err := deps.Storage.StatObject(verifyCtx, object.StorageKey)
			if err != nil {
				return apierrors.StorageUnavailable
			}
			if !exists {
				return apierrors.CalendarCoverConflict
			}
			if info.Size != int64(len(in.RawBody)) || !storage.SameMediaType(info.ContentType, contentType) ||
				(object.ByteSize != uint64(len(in.RawBody)) && blobref.Verified(object)) ||
				(blobref.Verified(object) && !storage.SameMediaType(object.ContentType, contentType)) {
				return apierrors.CalendarCoverConflict
			}
			storedDigest, err := deps.Storage.SHA256(verifyCtx, object.StorageKey)
			if err != nil {
				return apierrors.StorageUnavailable
			}
			if !bytes.Equal(storedDigest, digest[:]) {
				return apierrors.CalendarCoverConflict
			}
			object, err = blobref.MarkVerified(ctx, q, object, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      digest[:],
				ByteSize:    uint64(len(in.RawBody)),
				ContentType: contentType,
				StorageKey:  object.StorageKey,
			})
			if err != nil {
				if errors.Is(err, blobref.ErrUnverifiedElsewhere) {
					return apierrors.CalendarCoverConflict
				}
				return err
			}

			if !oldExists || oldObjectID != object.ID {
				if err := q.UpsertCalendarCoverImage(ctx, generated.UpsertCalendarCoverImageParams{
					CalendarID:      calendar.ID,
					StorageObjectID: object.ID,
				}); err != nil {
					return err
				}
				if err := blobref.Acquire(ctx, q, object.ID); err != nil {
					return err
				}
				if oldExists {
					if err := blobref.Release(ctx, q, oldObjectID); err != nil {
						return err
					}
				}
				changed = true
			}
			if changed {
				if err := eventlog.Append(ctx, q, eventlog.Entry{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  calendar.ID,
					ActorUserID: userID,
					Type:        eventlog.TypeCalendarEdited,
					Summary:     current.Name,
					Extra:       map[string]any{"calendarId": eventlog.UUID(calendar.PublicID)},
				}); err != nil {
					return err
				}
			}
			updated, err = q.GetCalendarByID(ctx, calendar.ID)
			return err
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		key, err := calendarCoverKey(ctx, deps.Queries, calendar.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		response := mapCalendarWithCover(deps, updated, member.Role, member.MemberColor, key)
		response.PublicShared = publicShared(ctx, deps, calendar.ID)
		return &PutCalendarCoverOutput{Body: response}, nil
	}
}

// DeleteCalendarCover removes the uploaded cover reference and returns the
// calendar in its current shared state.
func DeleteCalendarCover(deps Deps) func(context.Context, *DeleteCalendarCoverInput) (*DeleteCalendarCoverOutput, error) {
	return func(ctx context.Context, in *DeleteCalendarCoverInput) (*DeleteCalendarCoverOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		calendar, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if !calendarAllowsUploadedCover(calendar.Kind, calendar.OwnerUserID) {
			return nil, apierrors.ToHuma(apierrors.CalendarCoverForbidden)
		}

		var updated generated.Calendar
		var member generated.CalendarMember
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, calendar.ID); err != nil {
				return err
			}
			var err error
			if member, err = calresolve.LockManage(ctx, q, calendar.ID, userID); err != nil {
				return err
			}
			current, err := q.GetCalendarByIDForUpdate(ctx, calendar.ID)
			if err != nil {
				return err
			}
			if !calendarAllowsUploadedCover(current.Kind, current.OwnerUserID) {
				return apierrors.CalendarCoverForbidden
			}

			objectID, err := q.GetCalendarCoverObjectID(ctx, calendar.ID)
			if errors.Is(err, sql.ErrNoRows) {
				updated = current
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := q.GetStorageObjectByIDForUpdate(ctx, objectID); err != nil {
				return err
			}
			if err := q.DeleteCalendarCoverImage(ctx, calendar.ID); err != nil {
				return err
			}
			if err := blobref.Release(ctx, q, objectID); err != nil {
				return err
			}
			if err := eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  calendar.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeCalendarEdited,
				Summary:     current.Name,
				Extra:       map[string]any{"calendarId": eventlog.UUID(calendar.PublicID)},
			}); err != nil {
				return err
			}
			updated = current
			return nil
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		response := mapCalendarWithCover(deps, updated, member.Role, member.MemberColor, "")
		response.PublicShared = publicShared(ctx, deps, calendar.ID)
		return &DeleteCalendarCoverOutput{Body: response}, nil
	}
}
