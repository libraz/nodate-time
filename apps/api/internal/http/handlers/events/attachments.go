package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
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

// PresignUpload reserves an attachment and returns a URL to upload to.
//
// Two rows are written: a storage_objects row for the blob, keyed on its
// digest so identical bytes reuse one object, and an attachment row that
// starts disabled. The attachment takes no reference on the object until
// the upload is confirmed, so a presign nobody ever uses leaves the object
// at zero references for the sweep to collect.
func PresignUpload(deps Deps) func(context.Context, *PresignUploadInput) (*PresignUploadOutput, error) {
	return func(ctx context.Context, in *PresignUploadInput) (*PresignUploadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if in.Body.ByteSize > maxAttachmentSize || in.Body.ByteSize < 0 {
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
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
		objectPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// The key is scoped by workspace, not by calendar, because that is
		// the scope storage_objects is unique on: two calendars in one
		// workspace uploading identical bytes must resolve to the same row,
		// and a per-calendar key would leave the second one looking up a key
		// the deduplicated row does not carry.
		//
		// It is composed only of server-side values and the digest. The
		// client-supplied filename is stored on the attachment row (and
		// surfaced via Content-Disposition on download) but never
		// concatenated into the key, which would allow "../" traversal into
		// another namespace.
		storageKey := fmt.Sprintf("workspace/%s/%s", pubIDToHex(deps.WorkspacePublicID), hex.EncodeToString(digest))

		// Bytes another attachment already stands behind are never handed out
		// for overwriting. The key is the digest of the file, so anyone who
		// knows a file's bytes can ask for a presign at the same key from
		// their own calendar and PUT something else there -- and every
		// attachment in the workspace that resolved to that object would start
		// serving the replacement. A caller with the same digest gets the
		// object that is already there and confirms against it.
		alreadyStored := false
		if existing, err := deps.Queries.GetStorageObjectByKey(ctx, storageKey); err == nil && existing.RefCount > 0 {
			_, exists, serr := deps.Storage.StatObject(ctx, storageKey)
			if serr != nil {
				return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
			}
			alreadyStored = exists
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := q.CreateStorageObject(ctx, generated.CreateStorageObjectParams{
				PublicID:    objectPubID[:],
				WorkspaceID: sql.NullInt32{Int32: int32(deps.WorkspaceID), Valid: true},
				Sha256:      digest,
				ByteSize:    uint64(in.Body.ByteSize),
				ContentType: contentType,
				StorageKey:  storageKey,
			}); err != nil {
				return err
			}
			// Read the object back rather than using LastInsertId: on the
			// dedup path the upsert inserted nothing and the id that matters
			// belongs to the row that was already there.
			object, err := q.GetStorageObjectByKey(ctx, storageKey)
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
			slog.ErrorContext(ctx, "failed to reserve attachment", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &PresignUploadOutput{}
		out.Body.AttachmentID = attachPubID.String()
		if !alreadyStored {
			url, err := deps.Storage.PresignPut(ctx, storageKey, contentType, in.Body.ByteSize, 15*time.Minute)
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
// was declared. Only the attachment row is dropped: the object it points at
// may be shared with an attachment that uploaded the same bytes correctly,
// so deleting the blob here could break somebody else's file. An object
// nothing refers to is collected by the sweep instead.
//
// Best-effort: errors are logged rather than surfaced, since the caller is
// already returning the mismatch to the client.
func discardReservation(ctx context.Context, deps Deps, attachmentID, uploaderID uint32) {
	if err := deps.Queries.DeletePendingAttachment(ctx, generated.DeletePendingAttachmentParams{
		ID:         attachmentID,
		UploaderID: uploaderID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to delete mismatched attachment row", "attachmentID", attachmentID, "error", err)
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

		url, err := deps.Storage.PresignDownload(ctx, att.StorageKey, att.Filename, 5*time.Minute)
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
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.SoftDeleteAttachment(ctx, att.ID); err != nil {
				return err
			}
			if err := q.DecrementStorageObjectRefs(ctx, att.StorageObjectID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentGone, att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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
			discardReservation(ctx, deps, att.ID, att.UploaderID)
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
		}
		if uint64(info.Size) != att.ByteSize {
			discardReservation(ctx, deps, att.ID, att.UploaderID)
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		// The digest decided which object these bytes are stored as, so it has
		// to be checked against them rather than taken on trust. Without this
		// an upload can put anything at a key it names, and every later
		// attachment of the real file resolves to it.
		stored, err := deps.Storage.SHA256(ctx, att.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !bytes.Equal(stored, att.Sha256) {
			discardReservation(ctx, deps, att.ID, att.UploaderID)
			return nil, apierrors.ToHuma(apierrors.AttachmentDigestMismatch)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			res, err := q.ConfirmEventAttachment(ctx, generated.ConfirmEventAttachmentParams{
				ID:         att.ID,
				UploaderID: userID,
			})
			if err != nil {
				return err
			}
			// The update re-checks enabled = FALSE, so a second confirm of the
			// same reservation affects no rows -- which is what stops it from
			// taking a second reference on the blob and pinning it forever.
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return apierrors.AttachmentNotFound
			}
			if err := q.IncrementStorageObjectRefs(ctx, att.StorageObjectID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentAdded, att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &ConfirmAttachmentOutput{
			Body: mapAttachment(att.PublicID, att.Filename, att.ContentType, att.ByteSize, att.CreatedAt),
		}, nil
	}
}
