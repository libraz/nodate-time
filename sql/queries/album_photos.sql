-- name: CreateAlbumPhoto :execresult
-- Rows are created disabled and only become visible once ConfirmAlbumPhoto runs
-- after the object upload succeeds, so a presign that is never uploaded leaves
-- no dangling row pointing at a nonexistent object.
INSERT INTO album_photos (
  public_id, calendar_id, uploaded_by, event_id, caption, content_type, byte_size,
  width, height, storage_key, taken_at, enabled
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0);

-- name: ConfirmAlbumPhoto :execresult
UPDATE album_photos SET enabled = 1 WHERE id = ? AND uploaded_by = ? AND enabled = 0;

-- name: GetAlbumPhotoByPublicID :one
SELECT * FROM album_photos WHERE public_id = ?;

-- name: ListAlbumPhotosFirstPage :many
SELECT ap.*,
       u.public_id AS uploader_public_id,
       u.name AS uploader_name,
       u.avatar_storage_key AS uploader_avatar_key,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by
LEFT JOIN events e ON e.id = ap.event_id
WHERE ap.calendar_id = ? AND ap.enabled = 1
ORDER BY ap.taken_at DESC, ap.id DESC
LIMIT ?;

-- name: ListAlbumPhotosAfter :many
SELECT ap.*,
       u.public_id AS uploader_public_id,
       u.name AS uploader_name,
       u.avatar_storage_key AS uploader_avatar_key,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by
LEFT JOIN events e ON e.id = ap.event_id
WHERE ap.calendar_id = ?
  AND ap.enabled = 1
  AND (
    ap.taken_at < sqlc.arg(taken_before)
    OR (ap.taken_at = sqlc.arg(taken_before) AND ap.id < sqlc.arg(id_before))
  )
ORDER BY ap.taken_at DESC, ap.id DESC
LIMIT ?;

-- name: UpdateAlbumPhotoMeta :exec
UPDATE album_photos SET caption = ?, event_id = ? WHERE id = ?;

-- name: SoftDeleteAlbumPhoto :exec
UPDATE album_photos SET enabled = 0 WHERE id = ?;

-- name: ListAlbumPhotoStorageKeysByCalendar :many
SELECT storage_key FROM album_photos WHERE calendar_id = ?;

-- name: ListAbandonedAlbumPhotoStorageKeys :many
SELECT storage_key FROM album_photos WHERE enabled = 0 AND created_at < ?;

-- name: DeleteAbandonedAlbumPhotos :exec
DELETE FROM album_photos WHERE enabled = 0 AND created_at < ?;
