-- name: GetCalendarCoverObjectID :one
SELECT storage_object_id FROM calendar_cover_images WHERE calendar_id = ?;

-- name: ListCalendarCoverKeys :many
SELECT calendar_id, storage_key FROM v_calendar_cover_images
WHERE calendar_id IN (sqlc.slice('calendar_ids'));

-- name: UpsertCalendarCoverImage :exec
INSERT INTO calendar_cover_images (calendar_id, storage_object_id)
VALUES (?, ?)
ON DUPLICATE KEY UPDATE storage_object_id = VALUES(storage_object_id);

-- name: DeleteCalendarCoverImage :exec
DELETE FROM calendar_cover_images WHERE calendar_id = ?;

-- GetStorageObjectByPublicID finds the object a cover reservation named, within
-- the workspace and under its row lock: the confirm decides on its state.
-- name: GetStorageObjectByPublicID :one
SELECT * FROM storage_objects
WHERE workspace_id = ? AND public_id = ? AND enabled = TRUE
FOR UPDATE;
