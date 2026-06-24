-- name: ListEventAttachments :many
SELECT * FROM event_attachments
WHERE event_id = ? AND enabled = 1
ORDER BY created_at;

-- name: GetAttachmentByPublicID :one
SELECT * FROM event_attachments WHERE public_id = ?;

-- name: CreateEventAttachment :execresult
-- Created disabled; ConfirmEventAttachment enables it once the upload lands, so
-- an abandoned presign never leaves a row pointing at a missing object.
INSERT INTO event_attachments (public_id, event_id, uploaded_by, filename, content_type, byte_size, storage_key, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, 0);

-- name: ConfirmEventAttachment :execresult
UPDATE event_attachments SET enabled = 1 WHERE id = ? AND enabled = 0;

-- name: SoftDeleteAttachment :exec
UPDATE event_attachments SET enabled = 0 WHERE id = ?;
