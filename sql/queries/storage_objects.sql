-- Blob metadata. The bytes live in object storage; this table is the
-- index, and ref_count is what decides when an object may be swept.

-- CreateStorageObject is content-addressed: the unique key is (scope, sha256),
-- so re-uploading identical bytes finds the existing row instead of writing a
-- second copy. ref_count stays at whatever it was; the caller increments it
-- when it attaches the object to something.
-- name: CreateStorageObject :execresult
INSERT INTO storage_objects (public_id, workspace_id, owner_user_id, sha256, byte_size, content_type, storage_key, ref_count)
VALUES (?, ?, ?, ?, ?, ?, ?, 0)
ON DUPLICATE KEY UPDATE id = id;

-- name: GetStorageObjectByID :one
SELECT * FROM storage_objects WHERE id = ? AND enabled = TRUE;

-- GetStorageObjectByWorkspaceDigest and GetStorageObjectByOwnerDigest find the
-- row that content addressing deduplicated onto. (scope, sha256) is the unique
-- key the upsert collides on, so it answers whatever key the row carries: an
-- album photo's bytes are written at a key chosen before anything knows the
-- digest, so the row holding them is keyed by whichever photo got there first.
--
-- FOR UPDATE because the caller's next step depends on the row's state --
-- whether its bytes were verified -- and a concurrent confirm may be changing
-- it.
-- name: GetStorageObjectByWorkspaceDigest :one
SELECT * FROM storage_objects
WHERE workspace_id = ? AND sha256 = ? AND enabled = TRUE
FOR UPDATE;

-- name: GetStorageObjectByOwnerDigest :one
SELECT * FROM storage_objects
WHERE owner_user_id = ? AND sha256 = ? AND enabled = TRUE
FOR UPDATE;

-- MarkStorageObjectUploaded records that the bytes behind a row were read back
-- and matched its digest, which is what makes the row something another
-- referrer may reuse. The verified size, type and key replace what the
-- reservation declared; a row already verified is left as it is.
-- name: MarkStorageObjectUploaded :exec
UPDATE storage_objects
SET storage_key = ?, byte_size = ?, content_type = ?, uploaded_at = CURRENT_TIMESTAMP(3)
WHERE id = ? AND uploaded_at IS NULL;

-- name: IncrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = ref_count + 1 WHERE id = ?;

-- DecrementStorageObjectRefs floors at zero. An unsigned column would
-- wrap on an extra release, turning "nothing refers to this" into a count
-- of four billion and pinning the object forever.
-- name: DecrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = GREATEST(ref_count, 1) - 1 WHERE id = ?;

-- The cursor matters more than the limit: an object that refuses to delete
-- would otherwise head every page and spend the whole batch budget. Which
-- objects are unreferenced is decided by storage_objects_unreferenced, the
-- same view every delete below reads.
-- name: ListUnreferencedStorageObjects :many
SELECT so.* FROM storage_objects so
INNER JOIN storage_objects_unreferenced u ON u.id = so.id
WHERE so.created_at < ?
  AND so.id > ?
ORDER BY so.id
LIMIT ?;

-- DeleteStorageObject removes a listed object, re-checking through the view
-- so an object that gained a referrer since it was listed is left alone.
-- name: DeleteStorageObject :execresult
DELETE so FROM storage_objects so
INNER JOIN storage_objects_unreferenced u ON u.id = so.id
WHERE so.id = ?;

-- DeleteUnreferencedStorageObject collects one named object, and only if
-- nothing holds it. It is what lets the caller that released the last
-- reference finish the job in the same pass, rather than leaving the bytes to
-- an age-gated sweep -- that gate exists to protect a reservation which has
-- not been confirmed yet, and an object whose last referrer was just deleted
-- cannot be one.
-- name: DeleteUnreferencedStorageObject :execresult
DELETE so FROM storage_objects so
INNER JOIN storage_objects_unreferenced u ON u.id = so.id
WHERE so.id = ?;

-- CountStorageObjectsByKey answers whether any object claims a storage key,
-- ignoring enabled: a disabled row still describes bytes this table is the
-- index for, and deleting them would leave it pointing at nothing.
-- name: CountStorageObjectsByKey :one
SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?;
