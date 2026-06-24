-- name: CreateAlbumPhoto :execresult
-- Rows are created disabled and only become visible once ConfirmAlbumPhoto runs
-- after the object upload succeeds, so a presign that is never uploaded leaves
-- no dangling row pointing at a nonexistent object.
INSERT INTO album_photos (
  public_id, calendar_id, uploaded_by, event_id, caption, content_type, byte_size,
  width, height, storage_key, taken_at, enabled
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0);

-- name: ConfirmAlbumPhoto :execresult
UPDATE album_photos SET enabled = 1 WHERE id = ? AND enabled = 0;

-- name: GetAlbumPhotoByPublicID :one
SELECT * FROM album_photos WHERE public_id = ?;

-- name: ListAlbumPhotosFirstPage :many
SELECT * FROM album_photos
WHERE calendar_id = ? AND enabled = 1
ORDER BY taken_at DESC, id DESC
LIMIT ?;

-- name: ListAlbumPhotosAfter :many
SELECT * FROM album_photos
WHERE calendar_id = ?
  AND enabled = 1
  AND (
    taken_at < sqlc.arg(taken_before)
    OR (taken_at = sqlc.arg(taken_before) AND id < sqlc.arg(id_before))
  )
ORDER BY taken_at DESC, id DESC
LIMIT ?;

-- name: UpdateAlbumPhotoMeta :exec
UPDATE album_photos SET caption = ?, event_id = ? WHERE id = ?;

-- name: SoftDeleteAlbumPhoto :exec
UPDATE album_photos SET enabled = 0 WHERE id = ?;
