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

-- Both listings join the uploader's avatar object, so a page carries the key
-- its pictures are signed from rather than costing a lookup per photo.
--
-- image_storage_key is where a photo's bytes are: the storage object once the
-- photo has been moved onto one, and the row's own key until then. The
-- fallback is expressed here as well as in the handler because a page holds
-- thirty photos, and resolving it per row is the lookup the avatar join above
-- exists to avoid.
--
-- thumbnail_storage_key is the grid-sized rendering, and is joined here for the
-- same reason: it is the key the tiles are actually drawn from, so resolving it
-- per row would put the page's whole saving back. It is NULL when the photo has
-- no thumbnail, which is normal -- the reader falls back to the picture itself.
-- name: ListAlbumPhotosFirstPage :many
SELECT ap.*,
       COALESCE(pso.storage_key, ap.storage_key) AS image_storage_key,
       tso.storage_key AS thumbnail_storage_key,
       u.public_id AS uploader_public_id,
       u.display_name AS uploader_display_name,
       u.avatar_url AS uploader_avatar_url,
       so.storage_key AS uploader_avatar_storage_key,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by_user_id
LEFT JOIN storage_objects pso ON pso.id = ap.storage_object_id
LEFT JOIN storage_objects tso ON tso.id = ap.thumbnail_object_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
LEFT JOIN calendar_events e ON e.id = ap.calendar_event_id
WHERE ap.calendar_id = ? AND ap.enabled = TRUE
ORDER BY ap.taken_at DESC, ap.id DESC
LIMIT ?;

-- name: ListAlbumPhotosAfter :many
SELECT ap.*,
       COALESCE(pso.storage_key, ap.storage_key) AS image_storage_key,
       tso.storage_key AS thumbnail_storage_key,
       u.public_id AS uploader_public_id,
       u.display_name AS uploader_display_name,
       u.avatar_url AS uploader_avatar_url,
       so.storage_key AS uploader_avatar_storage_key,
       e.public_id AS event_public_id
FROM album_photos ap
INNER JOIN users u ON u.id = ap.uploaded_by_user_id
LEFT JOIN storage_objects pso ON pso.id = ap.storage_object_id
LEFT JOIN storage_objects tso ON tso.id = ap.thumbnail_object_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
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
--
-- storage_object_id comes along because deleting the row is what ends the
-- photo's reference to its blob: the sweep releases it there, which is the
-- one place every way of losing a photo passes through. thumbnail_object_id is
-- read for the same reason -- a photo holds up to two objects and the row going
-- away ends both claims, so a sweep that released only the first would pin the
-- thumbnail's bytes for good.
-- name: ListAbandonedAlbumPhotoStorageKeys :many
SELECT id, storage_key, storage_object_id, thumbnail_object_id FROM album_photos
WHERE enabled = FALSE AND updated_at < ? AND id > ?
ORDER BY id
LIMIT ?;

-- AttachAlbumPhotoStorageObject moves a photo onto the object model. The
-- IS NULL guard is what makes it idempotent: a confirm and a backfill pass
-- racing for the same row both write, but only one reports a row, and only
-- that one takes the reference.
-- name: AttachAlbumPhotoStorageObject :execresult
UPDATE album_photos SET storage_object_id = ?
WHERE id = ? AND storage_object_id IS NULL;

-- AttachAlbumPhotoThumbnailObject points a photo at the object holding its
-- grid-sized rendering. The IS NULL guard is the one AttachAlbumPhotoStorageObject
-- uses, and for the same reason: two confirms racing over one photo both write,
-- one reports a row, and only that one takes the reference.
-- name: AttachAlbumPhotoThumbnailObject :execresult
UPDATE album_photos SET thumbnail_object_id = ?
WHERE id = ? AND thumbnail_object_id IS NULL;

-- ListAlbumPhotosWithoutStorageObject walks the photos that predate the
-- object model, oldest first by id so the cursor makes progress.
--
-- Only live photos are backfilled. A retired one is on its way out through
-- the sweep above, and moving it onto an object first would take a reference
-- that the same sweep has to release moments later.
-- name: ListAlbumPhotosWithoutStorageObject :many
SELECT id, workspace_id, storage_key, content_type, byte_size FROM album_photos
WHERE storage_object_id IS NULL AND enabled = TRUE AND storage_key <> '' AND id > ?
ORDER BY id
LIMIT ?;

-- name: DeleteAbandonedAlbumPhoto :execresult
DELETE FROM album_photos WHERE id = ? AND enabled = FALSE;
