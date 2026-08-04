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

-- name: ListAttachmentObjectIDsByEvent :many
SELECT storage_object_id FROM calendar_event_attachments WHERE event_id = ?;

-- name: ListAttachmentObjectIDsByCalendar :many
SELECT a.storage_object_id
FROM calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
WHERE e.calendar_id = ?;

-- name: ListAbandonedAttachments :many
SELECT a.id, a.storage_object_id
FROM calendar_event_attachments a
WHERE a.enabled = FALSE AND a.created_at < ?
ORDER BY a.id
LIMIT ?;
