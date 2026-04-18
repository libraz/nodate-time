-- name: ListEventAttachments :many
SELECT * FROM event_attachments
WHERE event_id = ? AND enabled = 1
ORDER BY created_at;

-- name: GetAttachmentByPublicID :one
SELECT * FROM event_attachments WHERE public_id = ?;

-- name: CreateEventAttachment :execresult
INSERT INTO event_attachments (public_id, event_id, uploaded_by, filename, content_type, byte_size, storage_key)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: SoftDeleteAttachment :exec
UPDATE event_attachments SET enabled = 0 WHERE id = ?;
