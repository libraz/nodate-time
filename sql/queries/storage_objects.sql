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

-- name: GetStorageObjectByKey :one
SELECT * FROM storage_objects WHERE storage_key = ? AND enabled = TRUE;

-- name: IncrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = ref_count + 1 WHERE id = ?;

-- DecrementStorageObjectRefs floors at zero. An unsigned column would
-- wrap on an extra release, turning "nothing refers to this" into a count
-- of four billion and pinning the object forever.
-- name: DecrementStorageObjectRefs :exec
UPDATE storage_objects SET ref_count = GREATEST(ref_count, 1) - 1 WHERE id = ?;

-- The cursor matters more than the limit. The attachments foreign key is
-- RESTRICT, so an object a row still points at cannot be deleted; reading each
-- page from the head of the table would put that same object first every time
-- and the sweep would spend its whole batch budget on it, never reaching the
-- rest of the backlog.
-- name: ListUnreferencedStorageObjects :many
SELECT * FROM storage_objects
WHERE ref_count = 0 AND created_at < ? AND id > ?
ORDER BY id
LIMIT ?;

-- name: DeleteStorageObject :execresult
DELETE FROM storage_objects WHERE id = ? AND ref_count = 0;

-- CountStorageObjectsByKey answers whether any object claims a storage key,
-- ignoring enabled: a disabled row still describes bytes this table is the
-- index for, and deleting them would leave it pointing at nothing.
-- name: CountStorageObjectsByKey :one
SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?;
