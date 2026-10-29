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

// attachmentUploadKey is where one reservation uploads bytes that are already
// stored in an object the uploader cannot read. A digest names bytes but does
// not prove having them, so reusing the stored object takes either read access
// to something holding it or sending the bytes; and they cannot be sent to the
// object's own key, which is never written twice. The key is the
// reservation's alone, so nothing else can be replaced through it.
func attachmentUploadKey(deps Deps, attachmentPublicID []byte) string {
	return fmt.Sprintf("workspace/%s/uploads/%s", pubIDToHex(deps.WorkspacePublicID), pubIDToHex(attachmentPublicID))
}

// canReadObject reports whether userID can already read the bytes of object
// through a calendar they belong to.
func canReadObject(ctx context.Context, q *generated.Queries, objectID, userID uint32) (bool, error) {
	readable, err := q.CanReadObject(ctx, generated.CanReadObjectParams{
		UserID:        userID,
		ObjectID:      objectID,
		PhotoObjectID: sql.NullInt32{Int32: int32(objectID), Valid: true},
	})
	return readable.Valid && readable.Bool, err
}

// PresignUpload reserves an attachment and returns a URL to upload to.
//
// Two rows are written: a storage_objects row for the blob, keyed on its
// digest so identical bytes reuse one object, and an attachment row that
// starts disabled. The attachment row counts towards the object's references
// from the moment it exists (blobref.Reserve takes it in the same
// transaction); a presign nobody ever uses is reclaimed by the cleanup sweep,
// which deletes the row and gives the reference back.
//
// The object may already exist -- as another attachment, or as an album photo
// whose bytes sit at the photo's own key. A caller who can read it already
// confirms without uploading; anyone else uploads to a key of the
// reservation's own, and the confirm checks those bytes against the digest.
func PresignUpload(deps Deps) func(context.Context, *PresignUploadInput) (*PresignUploadOutput, error) {
	return func(ctx context.Context, in *PresignUploadInput) (*PresignUploadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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

		var (
			object   generated.StorageObject
			mustSend bool
		)
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEventEdit(ctx, q, cal, evt.ID, userID); err != nil {
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
			if blobref.Verified(object) {
				readable, err := canReadObject(ctx, q, object.ID, userID)
				if err != nil {
					return err
				}
				mustSend = !readable
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

		out := &PresignUploadOutput{}
		out.Body.AttachmentID = attachPubID.String()
		if mustSend {
			url, err := deps.Storage.SignedPutURL(attachmentUploadKey(deps, attachPubID[:]), contentType, in.Body.ByteSize, 15*time.Minute)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
			}
			out.Body.UploadURL = url
			return out, nil
		}

		// Bytes already at the key are not handed out for writing again: the
		// relay never overwrites a stored key, so the upload could only fail.
		// The caller confirms against what is there, and the confirm's digest
		// check is what decides whether it is the file they meant.
		_, alreadyStored, err := deps.Storage.StatObject(ctx, object.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
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
// was declared. The attachment row is dropped and gives back its reference in
// the same transaction; the object row is left for the sweep, because other
// reservations of the same digest may point at it.
//
// The bytes are queued for removal too. Those at the reservation's own upload
// key go unconditionally: nothing else can be stored there. Those at the
// object's key go while the object is still unverified: they are at a key
// derived from a digest they do not match, so nobody can confirm against
// them, and the relay never overwrites a key -- left in place they would
// block every honest upload of that file. The drainer never removes a key a
// verified object claims. They are removed right after the commit rather than
// left to the drainer, so the real file can be uploaded straight away.
//
// Best-effort: errors are logged rather than surfaced, since the caller is
// already returning the mismatch to the client.
func discardReservation(ctx context.Context, deps Deps, att generated.GetPendingAttachmentByPublicIDRow, key string) {
	var queued []string
	if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		queued = nil
		res, err := q.DeletePendingAttachment(ctx, generated.DeletePendingAttachmentParams{
			ID:         att.ID,
			UploaderID: att.UploaderID,
		})
		if err != nil {
			return err
		}
		if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
			return err
		}
		if err := blobref.Release(ctx, q, att.StorageObjectID); err != nil {
			return err
		}
		if key == att.StorageKey {
			object, err := q.GetStorageObjectByID(ctx, att.StorageObjectID)
			if err != nil || blobref.Verified(object) || object.StorageKey != att.StorageKey {
				return err
			}
		}
		queued = []string{key}
		return blobref.QueueRemoval(ctx, q, key)
	}); err != nil {
		slog.WarnContext(ctx, "failed to discard mismatched attachment reservation", "attachmentID", att.ID, "error", err)
		return
	}
	if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, queued...); err != nil {
		slog.WarnContext(ctx, "failed to remove mismatched attachment bytes; left to the drainer", "key", key, "error", err)
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
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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

		// Retire the row and leave the blob alone. The retired row keeps its
		// reference until the cleanup sweep removes it, and the object is
		// content-addressed, so the same bytes may back another attachment
		// entirely.
		//
		// Only the delete that actually retired the row logs it: a second one
		// racing it read the same live row, but its update finds it retired.
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEventEdit(ctx, q, cal, evt.ID, userID); err != nil {
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
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentDeleted, "attachmentId", att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &DeleteAttachmentOutput{}, nil
	}
}

// ConfirmAttachment finalizes a presigned attachment upload: it verifies the
// object actually landed in storage, then enables the row and marks the object
// verified. The reference was taken when the reservation was created. An
// abandoned presign never enables, so it leaves no visible attachment pointing
// at a missing object.
func ConfirmAttachment(deps Deps) func(context.Context, *ConfirmAttachmentInput) (*ConfirmAttachmentOutput, error) {
	return func(ctx context.Context, in *ConfirmAttachmentInput) (*ConfirmAttachmentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// Refused callers are told so before anything about the reservation or
		// the bytes is looked at; the confirm's own transaction decides again.
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			_, err := lockEventEdit(ctx, q, cal, evt.ID, userID)
			return err
		}); err != nil {
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

		// Bytes the reservation sent to its own key are what it confirms; with
		// none there it confirms against the object's key, which takes read
		// access to the object (checked below, under the lock).
		key := attachmentUploadKey(deps, att.PublicID)
		_, sent, err := deps.Storage.StatObject(ctx, key)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !sent {
			key = att.StorageKey
		}
		info, exists, err := deps.Storage.StatObject(ctx, key)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !exists {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if info.Size > maxAttachmentSize {
			discardReservation(ctx, deps, att, key)
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
		}
		// The digest decided which object these bytes are stored as, so it has
		// to be checked against them rather than taken on trust. Without this
		// an upload can put anything at a key it names, and every later
		// attachment of the real file resolves to it. It is also the size
		// check: the declared size on an unverified object is whatever the
		// first reservation said, and the bytes that match the digest are the
		// ones whose size is recorded below.
		stored, err := deps.Storage.SHA256(ctx, key)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !bytes.Equal(stored, att.Sha256) {
			discardReservation(ctx, deps, att, key)
			return nil, apierrors.ToHuma(apierrors.AttachmentDigestMismatch)
		}

		var (
			object generated.StorageObject
			queued []string
		)
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			queued = nil
			if _, err := lockEventEdit(ctx, q, cal, evt.ID, userID); err != nil {
				if errors.Is(err, apierrors.EventNotFound) {
					return apierrors.AttachmentNotFound
				}
				return err
			}
			// Verified bytes at the object's key are somebody's stored file:
			// confirming against them without having sent them takes being able
			// to read them already.
			if !sent {
				reserved, err := q.GetStorageObjectByID(ctx, att.StorageObjectID)
				if err != nil {
					return err
				}
				if blobref.Verified(reserved) {
					readable, err := canReadObject(ctx, q, reserved.ID, userID)
					if err != nil {
						return err
					}
					if !readable {
						return apierrors.AttachmentNotFound
					}
				}
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
			// with the parent's delete, affects no rows.
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
				StorageKey:  key,
			})
			if err != nil {
				return err
			}
			if object.ID != att.StorageObjectID {
				return fmt.Errorf("attachment %d points at object %d, digest resolves to %d",
					att.ID, att.StorageObjectID, object.ID)
			}
			// Another upload verified the same bytes first -- an album photo
			// stored at its own key, or the object this reservation sent its
			// copy alongside -- so the copy at the key this confirm read is
			// held by nothing. Queued here so the record commits with the
			// confirm, and removed right after it; neither touches a key a
			// verified object claims.
			copies := []string{attachmentStorageKey(deps, att.Sha256)}
			if key != copies[0] {
				copies = append(copies, key)
			}
			for _, copyKey := range copies {
				if object.StorageKey == copyKey {
					continue
				}
				if err := blobref.QueueRemoval(ctx, q, copyKey); err != nil {
					return err
				}
				queued = append(queued, copyKey)
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeAttachmentCreated, "attachmentId", att.Filename, att.PublicID)
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, queued...); err != nil {
			slog.WarnContext(ctx, "failed to remove superseded attachment bytes; left to the drainer", "error", err)
		}

		return &ConfirmAttachmentOutput{
			Body: mapAttachment(att.PublicID, att.Filename, object.ContentType, object.ByteSize, att.CreatedAt),
		}, nil
	}
}
