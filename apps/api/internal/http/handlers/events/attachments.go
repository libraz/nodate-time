package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

const maxAttachmentSize = 100 * 1024 * 1024 // 100 MB

// isRejectedAttachmentContentType blocks SVG, which browsers can render
// inline with active content (<script>) when opened directly. mime.ParseMediaType
// strips parameters first, so "image/svg+xml; charset=utf-8" cannot slip past
// this the way it could past a plain string-equality check.
func isRejectedAttachmentContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// An unparsable content type is passed through as-is (attachments accept
		// arbitrary types by default); only a confirmed SVG is rejected here.
		return strings.EqualFold(strings.TrimSpace(contentType), "image/svg+xml")
	}
	return strings.EqualFold(mediaType, "image/svg+xml")
}

// parseSHA256 accepts the digest the client computed over the bytes it is
// about to upload. It is what makes the blob content-addressed: the same
// file attached to two events is stored once and referred to twice.
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

func mapAttachment(publicID []byte, filename, contentType string, byteSize uint64, createdAt time.Time) AttachmentResponse {
	return AttachmentResponse{
		ID:          pubIDToHex(publicID),
		Filename:    filename,
		ContentType: contentType,
		ByteSize:    int64(byteSize),
		CreatedAt:   createdAt,
	}
}

// attachmentStorageKey is where an attachment upload of these bytes is
// written when no object holds them yet.
//
// The key is scoped by workspace, not by calendar, because that is the scope
// storage_objects is unique on: two calendars in one workspace uploading
// identical bytes resolve to the same row.
//
// It is composed only of server-side values and the digest. The
// client-supplied filename is stored on the attachment row (and surfaced via
// Content-Disposition on download) but never concatenated into the key, which
// would allow "../" traversal into another namespace.
func attachmentStorageKey(deps Deps, digest []byte) string {
	return fmt.Sprintf("workspace/%s/%s", pubIDToHex(deps.WorkspacePublicID), hex.EncodeToString(digest))
}

// PresignUpload reserves an attachment and returns a URL to upload to.
//
// Two rows are written: a storage_objects row for the blob, keyed on its
// digest so identical bytes reuse one object, and an attachment row that
// starts disabled. The attachment takes no reference on the object until
// the upload is confirmed, so a presign nobody ever uses leaves the object
// at zero references for the sweep to collect.
//
// The object may already exist -- as another attachment, or as an album photo
// whose bytes sit at the photo's own key -- and the upload URL, if any, is for
// whichever key that object carries.
func PresignUpload(deps Deps) func(context.Context, *PresignUploadInput) (*PresignUploadOutput, error) {
	return func(ctx context.Context, in *PresignUploadInput) (*PresignUploadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if in.Body.ByteSize > maxAttachmentSize {
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
		}
		// A size the upload URL cannot be signed for is refused here rather
		// than passed on: the signature is what bounds the upload, so a URL
		// issued without one accepts an object of any size.
		if in.Body.ByteSize < 1 {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		digest, ok := parseSHA256(in.Body.SHA256)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		contentType := in.Body.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if isRejectedAttachmentContentType(contentType) {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		attachPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		storageKey := attachmentStorageKey(deps, digest)

		var object generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				return err
			}
			var err error
			object, err = blobref.Reserve(ctx, q, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      digest,
				ByteSize:    uint64(in.Body.ByteSize),
				ContentType: contentType,
				StorageKey:  storageKey,
			})
			if err != nil {
				return err
			}
			_, err = q.CreateEventAttachment(ctx, generated.CreateEventAttachmentParams{
				PublicID:        attachPubID[:],
				WorkspaceID:     deps.WorkspaceID,
				EventID:         sql.NullInt32{Int32: int32(evt.ID), Valid: true},
				UploaderID:      userID,
				StorageObjectID: object.ID,
				Filename:        in.Body.Filename,
			})
			return err
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to reserve attachment", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Bytes already at the key are not handed out for writing again: the
		// relay never overwrites a stored key, so the upload could only fail.
		// The caller confirms against what is there, and the confirm's digest
		// check is what decides whether it is the file they meant.
		_, alreadyStored, err := deps.Storage.StatObject(ctx, object.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &PresignUploadOutput{}
		out.Body.AttachmentID = attachPubID.String()
		if !alreadyStored {
			url, err := deps.Storage.SignedPutURL(object.StorageKey, contentType, in.Body.ByteSize, 15*time.Minute)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
			}
			out.Body.UploadURL = url
		}
		return out, nil
	}
}

// ListAttachments returns all active attachments for an event.
func ListAttachments(deps Deps) func(context.Context, *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
	return func(ctx context.Context, in *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListEventAttachments(ctx, sql.NullInt32{Int32: int32(evt.ID), Valid: true})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListAttachmentsOutput{Body: make([]AttachmentResponse, 0, len(rows))}
		for _, att := range rows {
			out.Body = append(out.Body, mapAttachment(att.PublicID, att.Filename, att.ContentType, att.ByteSize, att.CreatedAt))
		}
		return out, nil
	}
}

// discardReservation removes a reservation whose upload did not match what
// was declared. The attachment row is dropped; the object row is left for the
// sweep, because other reservations of the same digest may point at it.
//
// The bytes go too while the object is still unverified. They are at a key
// derived from a digest they do not match, so nobody can confirm against them,
// and the relay never overwrites a key -- left in place they would block every
// honest upload of that file. A verified object's bytes are never touched.
//
// Best-effort: errors are logged rather than surfaced, since the caller is
// already returning the mismatch to the client.
func discardReservation(ctx context.Context, deps Deps, att generated.GetPendingAttachmentByPublicIDRow) {
	if err := deps.Queries.DeletePendingAttachment(ctx, generated.DeletePendingAttachmentParams{
		ID:         att.ID,
		UploaderID: att.UploaderID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to delete mismatched attachment row", "attachmentID", att.ID, "error", err)
	}
	object, err := deps.Queries.GetStorageObjectByID(ctx, att.StorageObjectID)
	if err != nil || blobref.Verified(object) || object.StorageKey != att.StorageKey {
		return
	}
	if err := deps.Storage.DeleteObject(ctx, att.StorageKey); err != nil {
		slog.WarnContext(ctx, "failed to delete mismatched attachment bytes", "key", att.StorageKey, "error", err)
	}
}

// GetAttachmentDownload generates a presigned download URL for an attachment.
func GetAttachmentDownload(deps Deps) func(context.Context, *GetAttachmentDownloadInput) (*GetAttachmentDownloadOutput, error) {
	return func(ctx context.Context, in *GetAttachmentDownloadInput) (*GetAttachmentDownloadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// The attachment must belong to an event in the resolved calendar.
		// Without this scoping check any member could download another tenant's
		// files by passing a foreign attachment UUID (cross-tenant IDOR).
		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		attPub, err := parseUUID(in.AttachmentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		att, err := deps.Queries.GetAttachmentByPublicID(ctx, attPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if !att.EventID.Valid || uint32(att.EventID.Int32) != evt.ID {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		url, err := deps.Storage.SignedDownloadURL(att.StorageKey, att.Filename, 5*time.Minute)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &GetAttachmentDownloadOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}

// DeleteAttachment soft-deletes an attachment from an event.
func DeleteAttachment(deps Deps) func(context.Context, *DeleteAttachmentInput) (*DeleteAttachmentOutput, error) {
	return func(ctx context.Context, in *DeleteAttachmentInput) (*DeleteAttachmentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		attPub, err := parseUUID(in.AttachmentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		att, err := deps.Queries.GetAttachmentByPublicID(ctx, attPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if !att.EventID.Valid || uint32(att.EventID.Int32) != evt.ID {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}

		// Release the reference and leave the blob alone. Objects are
		// content-addressed, so the same bytes may back another attachment
		// entirely; deleting the object here would break that one. The sweep
		// removes it once nothing refers to it.
		//
		// Only the delete that actually retired the row releases: a second one
		// racing it read the same live row, but its update finds it retired.
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				if errors.Is(err, apierrors.EventNotFound) {
					return apierrors.AttachmentNotFound
				}
				return err
			}
			res, err := q.SoftDeleteAttachment(ctx, att.ID)
			if err != nil {
				return err
			}
			retired, err := blobref.ChangedOne(res)
			if err != nil {
				return err
			}
			if !retired {
				return apierrors.AttachmentNotFound
			}
			if err := blobref.Release(ctx, q, att.StorageObjectID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentGone, att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &DeleteAttachmentOutput{}, nil
	}
}

// ConfirmAttachment finalizes a presigned attachment upload: it verifies the
// object actually landed in storage, then enables the row and takes a
// reference on the blob. An abandoned presign never enables, so it leaves no
// attachment pointing at a missing object.
func ConfirmAttachment(deps Deps) func(context.Context, *ConfirmAttachmentInput) (*ConfirmAttachmentOutput, error) {
	return func(ctx context.Context, in *ConfirmAttachmentInput) (*ConfirmAttachmentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		attPub, err := parseUUID(in.AttachmentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		att, err := deps.Queries.GetPendingAttachmentByPublicID(ctx, attPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if !att.EventID.Valid || uint32(att.EventID.Int32) != evt.ID {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if att.UploaderID != userID {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}

		info, exists, err := deps.Storage.StatObject(ctx, att.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !exists {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if info.Size > maxAttachmentSize {
			discardReservation(ctx, deps, att)
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
		}
		// The digest decided which object these bytes are stored as, so it has
		// to be checked against them rather than taken on trust. Without this
		// an upload can put anything at a key it names, and every later
		// attachment of the real file resolves to it. It is also the size
		// check: the declared size on an unverified object is whatever the
		// first reservation said, and the bytes that match the digest are the
		// ones whose size is recorded below.
		stored, err := deps.Storage.SHA256(ctx, att.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !bytes.Equal(stored, att.Sha256) {
			discardReservation(ctx, deps, att)
			return nil, apierrors.ToHuma(apierrors.AttachmentDigestMismatch)
		}

		var object generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				if errors.Is(err, apierrors.EventNotFound) {
					return apierrors.AttachmentNotFound
				}
				return err
			}
			res, err := q.ConfirmEventAttachment(ctx, generated.ConfirmEventAttachmentParams{
				ID:         att.ID,
				UploaderID: userID,
			})
			if err != nil {
				return err
			}
			// The update re-checks enabled = FALSE and that the event and
			// calendar are live, so a second confirm, or one that lost a race
			// with the parent's delete, affects no rows and takes no reference.
			confirmed, err := blobref.ChangedOne(res)
			if err != nil {
				return err
			}
			if !confirmed {
				return apierrors.AttachmentNotFound
			}
			object, err = blobref.Store(ctx, q, blobref.Blob{
				WorkspaceID: att.WorkspaceID,
				SHA256:      att.Sha256,
				ByteSize:    uint64(info.Size),
				ContentType: info.ContentType,
				StorageKey:  att.StorageKey,
			})
			if err != nil {
				return err
			}
			if object.ID != att.StorageObjectID {
				return fmt.Errorf("attachment %d points at object %d, digest resolves to %d",
					att.ID, att.StorageObjectID, object.ID)
			}
			if err := blobref.Acquire(ctx, q, object.ID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentAdded, att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		// An album photo verified the same bytes first and took the object
		// over onto its own key, so a copy uploaded to the attachment key is
		// held by nothing. Best-effort: left behind, it is only space.
		if ownKey := attachmentStorageKey(deps, att.Sha256); object.StorageKey != ownKey {
			if used, cerr := deps.Queries.CountStorageObjectsByKey(ctx, ownKey); cerr == nil && used == 0 {
				if derr := deps.Storage.DeleteObject(ctx, ownKey); derr != nil {
					slog.WarnContext(ctx, "failed to delete superseded attachment bytes", "key", ownKey, "error", derr)
				}
			}
		}

		return &ConfirmAttachmentOutput{
			Body: mapAttachment(att.PublicID, att.Filename, object.ContentType, object.ByteSize, att.CreatedAt),
		}, nil
	}
}
