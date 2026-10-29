-- Blob metadata. The bytes live in object storage; this table is the
-- index, and ref_count is what decides when an object may be swept.

-- CreateStorageObject is content-addressed: the unique key is (scope, sha256),
-- so re-uploading identical bytes finds the existing row instead of writing a
-- second copy. ref_count stays at whatever it was; the transaction that
-- inserts the row pointing at the object increments it (PROTOCOL.md section
-- Storage).
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

-- MarkStorageObjectUploaded records that the bytes at the row's own key were
-- read back and matched its digest, which is what makes the row something
-- another referrer may reuse. The verified size and type replace what the
-- reservation declared. storage_key is never rewritten: another product may
-- have handed out an upload URL for it. A row already verified is left as it
-- is.
-- name: MarkStorageObjectUploaded :exec
UPDATE storage_objects
SET byte_size = ?, content_type = ?, uploaded_at = CURRENT_TIMESTAMP(3)
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
-- same view every delete below reads. It lists confirmed objects only, so no
-- age condition is needed: a reservation is reclaimed by the unconfirmed
-- sweep alone.
-- name: ListUnreferencedStorageObjects :many
SELECT so.* FROM storage_objects so
INNER JOIN storage_objects_unreferenced u ON u.id = so.id
WHERE so.id > ?
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

-- ListUnconfirmedStorageObjects walks reservations whose upload never
-- arrived, in any layer. Aged by updated_at: taking a reference restamps it,
-- and a reference is taken each time an upload URL is handed out for the row,
-- so the age is measured from the newest URL. Paged by id so objects another
-- layer still holds, skipped on every pass, cannot fill every page.
-- name: ListUnconfirmedStorageObjects :many
SELECT id, storage_key FROM storage_objects
WHERE uploaded_at IS NULL AND updated_at < sqlc.arg(cutoff) AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT ?;

-- DeleteUnconfirmedStorageObject removes a reservation whose referrers in this
-- layer were deleted in the same transaction, guarded on the predicate that
-- listed it so a confirm landing in between wins. ref_count is not consulted:
-- the object goes with every row that counted towards it.
-- name: DeleteUnconfirmedStorageObject :execresult
DELETE FROM storage_objects WHERE id = ? AND uploaded_at IS NULL;

-- CountStorageObjectsByKey answers whether any object claims a storage key,
-- ignoring enabled: a disabled row still describes bytes this table is the
-- index for, and deleting them would leave it pointing at nothing.
-- name: CountStorageObjectsByKey :one
SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?;

-- CountConfirmedStorageObjectsByKey answers whether verified bytes at a key
-- belong to a live object. A queued removal for such a key is stale -- the
-- key was claimed again after it was queued -- and the drainer drops it
-- without touching the blob. An unconfirmed claim does not protect the key:
-- nothing has vouched for the bytes there.
-- name: CountConfirmedStorageObjectsByKey :one
SELECT COUNT(*) FROM storage_objects WHERE storage_key = ? AND uploaded_at IS NOT NULL;

-- QueueStorageBlobRemoval records a key whose blob is to be removed, in the
-- transaction that deletes the rows naming it. A key already queued stays one
-- row.
-- name: QueueStorageBlobRemoval :exec
INSERT INTO storage_blob_removals (storage_key)
VALUES (?)
ON DUPLICATE KEY UPDATE storage_key = VALUES(storage_key);

-- name: ListStorageBlobRemovals :many
SELECT id, storage_key FROM storage_blob_removals
WHERE id > ?
ORDER BY id
LIMIT ?;

-- name: DeleteStorageBlobRemoval :exec
DELETE FROM storage_blob_removals WHERE id = ?;

-- DeleteStorageBlobRemovalByKey drops the queued removal of a key whose blob
-- the request that queued it has just removed itself.
-- name: DeleteStorageBlobRemovalByKey :exec
DELETE FROM storage_blob_removals WHERE storage_key = ?;

-- InsertStorageLocationIfAbsent records the bucket this database's blobs live
-- in, unless a drainer of either product already did. The unique key on
-- setting_key makes the first writer win.
-- name: InsertStorageLocationIfAbsent :exec
INSERT INTO instance_settings (public_id, setting_key, setting_value)
VALUES (sqlc.arg(public_id), 'storage.location', sqlc.arg(location))
ON DUPLICATE KEY UPDATE setting_key = setting_key;

-- GetStorageLocation reads the recorded bucket regardless of enabled, so a
-- disabled row still blocks a drainer pointed elsewhere.
-- name: GetStorageLocation :one
SELECT setting_value FROM instance_settings
WHERE setting_key = 'storage.location'
LIMIT 1;
