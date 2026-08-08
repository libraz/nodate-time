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

-- GetStorageObjectByWorkspaceDigest finds the row that content addressing
-- deduplicated onto.
--
-- Reading it back by key only works when the key is derived from the digest,
-- which is true of attachments and avatars and not of album photos: their
-- bytes are written at a key chosen before anything knows the digest, so the
-- row already holding those bytes is keyed by whichever photo got there
-- first. (workspace_id, sha256) is the unique key the upsert collides on, so
-- it is the one that can answer for either.
-- name: GetStorageObjectByWorkspaceDigest :one
SELECT * FROM storage_objects
WHERE workspace_id = ? AND sha256 = ? AND enabled = TRUE;

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
--
-- A retired attachment row is still a row, and it holds its object until the
-- retention window passes. Those objects are excluded here rather than
-- attempted and logged: a foreign key doing its job every fifteen minutes is
-- not a warning, and burying the real failures under it costs more than the
-- extra clause.
--
-- There is one clause per column that can point at an object, and the caller
-- deletes by id without re-checking them, so a column added to any referring
-- table has to be added here too. Missing one does not corrupt anything --
-- the foreign keys are RESTRICT -- but the object is listed every tick,
-- refuses to delete every tick, and spends the batch budget it was the point
-- of this cursor to protect.
-- name: ListUnreferencedStorageObjects :many
SELECT * FROM storage_objects so
WHERE so.ref_count = 0
  AND so.created_at < ?
  AND so.id > ?
  AND NOT EXISTS (
    SELECT 1 FROM calendar_event_attachments a WHERE a.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.thumbnail_object_id = so.id
  )
ORDER BY so.id
LIMIT ?;

-- name: DeleteStorageObject :execresult
DELETE FROM storage_objects WHERE id = ? AND ref_count = 0;

-- DeleteUnreferencedStorageObject collects one named object, and only if
-- nothing holds it: the count is zero and no referring row of any kind is
-- still pointing at it. It is what lets the caller that released the last
-- reference finish the job in the same pass, rather than leaving the bytes to
-- an age-gated sweep -- that gate exists to protect a reservation which has
-- not been confirmed yet, and an object whose last referrer was just deleted
-- cannot be one.
--
-- The NOT EXISTS clauses are the same ones the sweep applies, one per column
-- that can point at an object. Every one of those foreign keys is RESTRICT,
-- so without them this is an error the caller has to interpret rather than an
-- answer.
-- name: DeleteUnreferencedStorageObject :execresult
DELETE so FROM storage_objects so
WHERE so.id = ?
  AND so.ref_count = 0
  AND NOT EXISTS (
    SELECT 1 FROM calendar_event_attachments a WHERE a.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.thumbnail_object_id = so.id
  );

-- CountStorageObjectsByKey answers whether any object claims a storage key,
-- ignoring enabled: a disabled row still describes bytes this table is the
-- index for, and deleting them would leave it pointing at nothing.
-- name: CountStorageObjectsByKey :one
SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?;
