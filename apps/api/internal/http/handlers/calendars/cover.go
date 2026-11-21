package calendars

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

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
	calendarCoverUploadTTL    = 15 * time.Minute
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

// coverBlock rejects a request that cannot concern an uploaded cover: the
// calendar is not one that takes one, or storage cannot serve it.
func coverBlock(deps Deps, calendar generated.Calendar) error {
	if !calendarAllowsUploadedCover(calendar.Kind, calendar.OwnerUserID) {
		return apierrors.CalendarCoverForbidden
	}
	if !calendarCoverStorageAvailable(deps) {
		return apierrors.StorageUnavailable
	}
	return nil
}

// uploadError maps a failed check of uploaded bytes to its Spec.
func uploadError(err error) error {
	if spec, ok := apierrors.FromBlobref(err); ok {
		return spec
	}
	return err
}

// stageCover finds or creates the object for the declared bytes under the
// calendar lock, refreshing an unconfirmed reservation. contentType and
// byteSize describe the reservation if it has to be created.
func stageCover(ctx context.Context, q *generated.Queries, deps Deps, calendarID, userID uint32, blob blobref.Blob) (generated.StorageObject, error) {
	if err := calresolve.LockExclusive(ctx, q, calendarID); err != nil {
		return generated.StorageObject{}, err
	}
	if _, err := calresolve.LockManage(ctx, q, calendarID, userID); err != nil {
		return generated.StorageObject{}, err
	}
	locked, err := q.GetCalendarByIDForUpdate(ctx, calendarID)
	if err != nil {
		return generated.StorageObject{}, err
	}
	if !calendarAllowsUploadedCover(locked.Kind, locked.OwnerUserID) {
		return generated.StorageObject{}, apierrors.CalendarCoverForbidden
	}
	staged, err := blobref.Stage(ctx, q, blob)
	if err != nil {
		return generated.StorageObject{}, err
	}
	// A pending object from an unrelated feature is not a key this endpoint is
	// allowed to write, even though the digest is shared.
	if !blobref.Verified(staged) && !strings.HasPrefix(staged.StorageKey, calendarCoverPath+"/") {
		return generated.StorageObject{}, apierrors.CalendarCoverConflict
	}
	return staged, nil
}

// PresignCalendarCover reserves the object a cover will be stored as and
// returns the URL to upload it to. The calendar does not change until
// PutCalendarCover confirms the upload.
func PresignCalendarCover(deps Deps) func(context.Context, *PresignCalendarCoverInput) (*PresignCalendarCoverOutput, error) {
	return func(ctx context.Context, in *PresignCalendarCoverInput) (*PresignCalendarCoverOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		calendar, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if err := coverBlock(deps, calendar); err != nil {
			return nil, toAPIError(err)
		}
		digest, err := blobref.ParseDigest(in.Body.SHA256)
		if err != nil {
			return nil, toAPIError(uploadError(err))
		}
		if in.Body.ByteSize > MaxCalendarCoverSize {
			return nil, apierrors.ToHuma(apierrors.CalendarCoverTooLarge)
		}
		storageKey, err := calendarCoverStorageKey()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var staged generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			var err error
			staged, err = stageCover(ctx, q, deps, calendar.ID, userID, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      digest,
				ByteSize:    uint64(in.Body.ByteSize),
				ContentType: in.Body.ContentType,
				StorageKey:  storageKey,
			})
			return err
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		out := &PresignCalendarCoverOutput{}
		out.Body.CoverID = uuid.UUID(staged.PublicID).String()
		if blobref.Verified(staged) {
			return out, nil
		}
		// The relay never overwrites a stored key, so bytes already there are
		// confirmed rather than offered for writing again.
		statCtx, cancel := context.WithTimeout(ctx, calendarCoverIOTimeout)
		defer cancel()
		_, stored, err := deps.Storage.StatObject(statCtx, staged.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if stored {
			return out, nil
		}
		url, err := deps.Storage.SignedPutURL(staged.StorageKey, in.Body.ContentType, in.Body.ByteSize, calendarCoverUploadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		out.Body.UploadURL = url
		return out, nil
	}
}

// PutCalendarCover confirms an uploaded image as the cover of the shared
// calendar: it reads the stored bytes back, checks them against the declared
// digest and the image rules, and attaches the object to the calendar.
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
		if err := coverBlock(deps, calendar); err != nil {
			return nil, toAPIError(err)
		}
		coverID, err := uuid.Parse(in.Body.CoverID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.NotFound)
		}

		var staged generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, calendar.ID); err != nil {
				return err
			}
			if _, err := calresolve.LockManage(ctx, q, calendar.ID, userID); err != nil {
				return err
			}
			var err error
			staged, err = q.GetStorageObjectByPublicID(ctx, generated.GetStorageObjectByPublicIDParams{
				WorkspaceID: sql.NullInt32{Int32: int32(deps.WorkspaceID), Valid: true},
				PublicID:    coverID[:],
			})
			if errors.Is(err, sql.ErrNoRows) {
				return apierrors.NotFound
			}
			if err == nil && !blobref.Verified(staged) && !strings.HasPrefix(staged.StorageKey, calendarCoverPath+"/") {
				return apierrors.CalendarCoverConflict
			}
			return err
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		// Storage I/O runs with no transaction open.
		digest := staged.Sha256
		data, contentType, err := readCover(ctx, deps, staged, digest)
		if err != nil {
			return nil, toAPIError(err)
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
				if objectID == staged.ID && errors.Is(err, sql.ErrNoRows) {
					// The staged object was swept after the bytes were checked.
					return apierrors.CalendarCoverConflict
				}
				if err != nil {
					return err
				}
				lockedObjects[objectID] = object
			}
			object, ok := lockedObjects[staged.ID]
			if !ok || !object.WorkspaceID.Valid || uint32(object.WorkspaceID.Int32) != deps.WorkspaceID ||
				!bytes.Equal(object.Sha256, digest) || object.StorageKey != staged.StorageKey {
				return apierrors.CalendarCoverConflict
			}
			// A verified row that went back to pending was reset after the I/O.
			if blobref.Verified(staged) && !blobref.Verified(object) {
				return apierrors.CalendarCoverConflict
			}
			if blobref.Verified(object) && (object.ByteSize != uint64(len(data)) || !storage.SameMediaType(object.ContentType, contentType)) {
				return apierrors.CalendarCoverConflict
			}
			object, err = blobref.MarkVerified(ctx, q, object, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      digest,
				ByteSize:    uint64(len(data)),
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
		response, err := withShareState(ctx, deps, mapCalendarWithCover(deps, updated, member.Role, member.MemberColor, key), calendar.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &PutCalendarCoverOutput{Body: response}, nil
	}
}

// readCover reads the bytes stored for the staged object and checks them: the
// digest the upload declared, the size bound, and that they are an image of an
// accepted format. It returns the bytes and their media type, and discards an
// unconfirmed upload whose bytes fail. It must run with no transaction open.
func readCover(ctx context.Context, deps Deps, staged generated.StorageObject, digest []byte) ([]byte, string, error) {
	ioCtx, cancel := context.WithTimeout(ctx, calendarCoverIOTimeout)
	defer cancel()
	obj, exists, err := deps.Storage.Open(ioCtx, staged.StorageKey)
	if err != nil {
		return nil, "", apierrors.StorageUnavailable
	}
	if !exists {
		return nil, "", apierrors.CalendarCoverConflict
	}
	defer obj.Close()
	data, err := io.ReadAll(io.LimitReader(obj, MaxCalendarCoverSize+1))
	if err != nil {
		return nil, "", apierrors.StorageUnavailable
	}
	fail := func(spec error) ([]byte, string, error) {
		if !blobref.Verified(staged) {
			discardCover(ctx, deps, staged)
		}
		return nil, "", spec
	}
	if err := blobref.VerifyReadBack(bytes.NewReader(data), digest, MaxCalendarCoverSize); err != nil {
		if spec, ok := apierrors.FromBlobref(err); ok {
			return fail(spec)
		}
		return nil, "", err
	}
	contentType, valid := sniffCalendarCover(data)
	if !valid {
		return fail(apierrors.CalendarCoverInvalid)
	}
	return data, contentType, nil
}

// discardCover drops an unconfirmed cover reservation whose bytes failed their
// checks, and queues the stored object for removal. A reservation another
// feature holds is left alone.
func discardCover(ctx context.Context, deps Deps, staged generated.StorageObject) {
	var queued bool
	err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		queued = false
		object, err := q.GetStorageObjectByIDForUpdate(ctx, staged.ID)
		if errors.Is(err, sql.ErrNoRows) || err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if blobref.Verified(object) || object.RefCount > 0 {
			return nil
		}
		if _, err := q.DeleteUnconfirmedStorageObject(ctx, object.ID); err != nil {
			return err
		}
		queued = true
		return blobref.QueueRemoval(ctx, q, object.StorageKey)
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to discard a rejected cover upload", "objectID", staged.ID, "error", err)
		return
	}
	if queued {
		if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, staged.StorageKey); err != nil {
			slog.WarnContext(ctx, "failed to remove a rejected cover upload; left to the drainer", "key", staged.StorageKey, "error", err)
		}
	}
}

// DeleteCalendarCover removes the uploaded cover reference.
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

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, calendar.ID); err != nil {
				return err
			}
			if _, err := calresolve.LockManage(ctx, q, calendar.ID, userID); err != nil {
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
			return nil
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		return &DeleteCalendarCoverOutput{}, nil
	}
}
