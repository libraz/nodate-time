-- name: CreateAlbumPhoto :execresult
-- Rows are created disabled and only become visible once ConfirmAlbumPhoto runs
-- after the object upload succeeds, so a presign that is never uploaded leaves
-- no dangling row pointing at a nonexistent object.
INSERT INTO album_photos (
  public_id, workspace_id, calendar_id, uploaded_by_user_id, calendar_event_id, caption, content_type, byte_size,
  width, height, storage_key, taken_at, enabled
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0);

-- name: ConfirmAlbumPhoto :execresult
UPDATE album_photos SET enabled = TRUE WHERE id = ? AND uploaded_by_user_id = ? AND enabled = FALSE;

-- name: GetAlbumPhotoByPublicID :one
SELECT * FROM album_photos WHERE public_id = ?;

-- name: ListAlbumPhotosFirstPage :many
SELECT ap.*,
       u.public_id AS uploader_public_id,
       u.display_name AS uploader_display_name,
       u.avatar_url AS uploader_avatar_url,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by_user_id
LEFT JOIN calendar_events e ON e.id = ap.calendar_event_id
WHERE ap.calendar_id = ? AND ap.enabled = TRUE
ORDER BY ap.taken_at DESC, ap.id DESC
LIMIT ?;

-- name: ListAlbumPhotosAfter :many
SELECT ap.*,
       u.public_id AS uploader_public_id,
       u.display_name AS uploader_display_name,
       u.avatar_url AS uploader_avatar_url,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by_user_id
LEFT JOIN calendar_events e ON e.id = ap.calendar_event_id
WHERE ap.calendar_id = ?
  AND ap.enabled = TRUE
  AND (
    ap.taken_at < sqlc.arg(taken_before)
    OR (ap.taken_at = sqlc.arg(taken_before) AND ap.id < sqlc.arg(id_before))
  )
ORDER BY ap.taken_at DESC, ap.id DESC
LIMIT ?;

-- name: UpdateAlbumPhotoMeta :exec
UPDATE album_photos SET caption = ?, calendar_event_id = ? WHERE id = ?;

-- name: SoftDeleteAlbumPhoto :exec
UPDATE album_photos SET enabled = FALSE WHERE id = ?;

-- name: DeletePendingAlbumPhoto :exec
-- Removes an unconfirmed (enabled = FALSE) row whose uploaded object failed the
-- Confirm-time size/type check, so it is not left as an orphan pointing at a
-- deleted object.
DELETE FROM album_photos WHERE id = ? AND uploaded_by_user_id = ? AND enabled = FALSE;

-- name: ListAlbumPhotoStorageKeysByCalendar :many
SELECT storage_key FROM album_photos WHERE calendar_id = ?;

-- name: ListAbandonedAlbumPhotoStorageKeys :many
SELECT storage_key FROM album_photos WHERE enabled = FALSE AND created_at < ?;

-- name: DeleteAbandonedAlbumPhotoByStorageKey :execresult
DELETE FROM album_photos
WHERE storage_key = ? AND enabled = FALSE AND created_at < ?;
