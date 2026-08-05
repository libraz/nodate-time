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

-- name: SoftDeleteAlbumPhotosByCalendar :exec
UPDATE album_photos SET enabled = FALSE WHERE calendar_id = ? AND enabled = TRUE;

-- ListAbandonedAlbumPhotoStorageKeys walks rows that are out of use: enabled
-- covers both a reservation whose upload never landed and a photo the user
-- deleted, and both are collected the same way.
--
-- The cutoff is on updated_at, which is when the row went out of use, not on
-- created_at. Ageing by creation time gets it backwards: a photo kept for a
-- year is collected on the very next pass after it is deleted, while one
-- uploaded and deleted this morning sits around until it is a year old.
-- name: ListAbandonedAlbumPhotoStorageKeys :many
SELECT id, storage_key FROM album_photos
WHERE enabled = FALSE AND updated_at < ? AND id > ?
ORDER BY id
LIMIT ?;

-- name: DeleteAbandonedAlbumPhoto :execresult
DELETE FROM album_photos WHERE id = ? AND enabled = FALSE;
