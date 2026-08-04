-- Blob metadata. The bytes live in object storage; this table is the
-- index, and ref_count is what decides when an object may be swept.

-- name: CreateStorageObject :execresult
INSERT INTO storage_objects (public_id, workspace_id, owner_user_id, byte_size, content_type, storage_key, ref_count)
VALUES (?, ?, ?, ?, ?, ?, 0);

-- name: GetStorageObjectByID :one
SELECT * FROM storage_objects WHERE id = ? AND enabled = TRUE;

-- name: GetStorageObjectByKey :one
SELECT * FROM storage_objects WHERE storage_key = ? AND enabled = TRUE;

-- name: IncrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = ref_count + 1 WHERE id = ?;

-- DecrementStorageObjectRefs floors at zero. An unsigned column would
-- wrap on an extra release, turning "nothing refers to this" into a count
-- of four billion and pinning the object forever.
-- name: DecrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = GREATEST(ref_count, 1) - 1 WHERE id = ?;

-- name: ListUnreferencedStorageObjects :many
SELECT * FROM storage_objects
WHERE ref_count = 0 AND created_at < ?
ORDER BY id
LIMIT ?;

-- name: DeleteStorageObject :execresult
DELETE FROM storage_objects WHERE id = ? AND ref_count = 0;
