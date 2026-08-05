-- Event attachments. The blob itself is a storage_objects row, shared by
-- reference, so the same upload attached twice is stored once.

-- name: ListEventAttachments :many
SELECT a.*, so.storage_key, so.content_type, so.byte_size
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.event_id = ? AND a.enabled = TRUE
ORDER BY a.created_at;

-- name: GetAttachmentByPublicID :one
SELECT a.*, so.storage_key, so.content_type, so.byte_size
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.public_id = ? AND a.enabled = TRUE;

-- GetPendingAttachmentByPublicID finds a reservation whose upload has not
-- been confirmed. It is a separate query because the enabled = TRUE filter
-- on the one above is what keeps an unconfirmed row out of every read path
-- -- and confirming is the one operation that has to see it.
-- name: GetPendingAttachmentByPublicID :one
SELECT a.*, so.storage_key, so.content_type, so.byte_size, so.sha256
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.public_id = ? AND a.enabled = FALSE;

-- CreateEventAttachment starts disabled. The row only becomes visible once
-- the upload has actually landed, so a presigned URL that is never used
-- leaves nothing pointing at an object that does not exist.
-- name: CreateEventAttachment :execresult
INSERT INTO calendar_event_attachments (public_id, workspace_id, event_id, uploader_id, storage_object_id, filename, enabled)
VALUES (?, ?, ?, ?, ?, ?, FALSE);

-- name: ConfirmEventAttachment :execresult
UPDATE calendar_event_attachments SET enabled = TRUE
WHERE id = ? AND uploader_id = ? AND enabled = FALSE;

-- name: SoftDeleteAttachment :exec
UPDATE calendar_event_attachments SET enabled = FALSE WHERE id = ?;

-- name: DeletePendingAttachment :exec
DELETE FROM calendar_event_attachments
WHERE id = ? AND uploader_id = ? AND enabled = FALSE;

-- Only a confirmed row holds a reference: an unconfirmed reservation never
-- incremented one, and a row already soft-deleted released its own. Listing
-- either would decrement a count nobody took, and because objects are
-- content-addressed and shared, that drives a blob another calendar is still
-- using down to zero and into the sweep.
-- name: ListAttachmentObjectIDsByEvent :many
SELECT storage_object_id FROM calendar_event_attachments
WHERE event_id = ? AND enabled = TRUE;

-- name: ListAttachmentObjectIDsByCalendar :many
SELECT a.storage_object_id
FROM calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
WHERE e.calendar_id = ? AND a.enabled = TRUE;

-- name: SoftDeleteAttachmentsByEvent :exec
UPDATE calendar_event_attachments SET enabled = FALSE
WHERE event_id = ? AND enabled = TRUE;

-- name: SoftDeleteAttachmentsByCalendar :exec
UPDATE calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
SET a.enabled = FALSE
WHERE e.calendar_id = ? AND a.enabled = TRUE;

-- DeleteAbandonedAttachment removes a reservation whose upload never
-- landed. It re-checks enabled = FALSE so a row confirmed between the
-- sweep listing it and this delete is left alone.
-- name: DeleteAbandonedAttachment :exec
DELETE FROM calendar_event_attachments WHERE id = ? AND enabled = FALSE;

-- ListAbandonedAttachments walks by id rather than re-reading the head of the
-- table: a row the delete below cannot remove would otherwise be listed again
-- on the next pass, and the sweep would spend its whole batch budget failing
-- on the same page.
-- name: ListAbandonedAttachments :many
SELECT a.id, a.storage_object_id
FROM calendar_event_attachments a
WHERE a.enabled = FALSE AND a.created_at < ? AND a.id > ?
ORDER BY a.id
LIMIT ?;

-- ListRetiredAttachments walks soft-deleted rows whose retention has run out.
--
-- A soft-deleted row keeps the file's name and uploader for the activity
-- history, but it still points at the blob through a RESTRICT foreign key, so
-- the object cannot be collected while it exists. Removing the row after a
-- retention window is what finally lets the sweep reclaim the bytes; the
-- reference itself was released when the row was soft-deleted.
-- name: ListRetiredAttachments :many
SELECT a.id
FROM calendar_event_attachments a
WHERE a.enabled = FALSE AND a.updated_at < ? AND a.id > ?
ORDER BY a.id
LIMIT ?;

-- name: DeleteRetiredAttachment :exec
DELETE FROM calendar_event_attachments WHERE id = ? AND enabled = FALSE;
