-- Event attachments. The blob itself is a storage_objects row, shared by
-- reference, so the same upload attached twice is stored once.

-- name: ListEventAttachments :many
SELECT a.*, so.storage_key, so.content_type, so.byte_size
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.event_id = ? AND a.enabled = TRUE
ORDER BY a.created_at;

-- name: GetAttachmentByPublicID :one
SELECT a.*, so.storage_key, so.content_type, so.byte_size
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.public_id = ? AND a.enabled = TRUE;

-- GetPendingAttachmentByPublicID finds a reservation whose upload has not
-- been confirmed. It is a separate query because the enabled = TRUE filter
-- on the one above is what keeps an unconfirmed row out of every read path
-- -- and confirming is the one operation that has to see it. A deleted
-- attachment is disabled too, and retired_at is what keeps it from reading
-- as a reservation and being confirmed back.
-- name: GetPendingAttachmentByPublicID :one
SELECT a.*, so.storage_key, so.content_type, so.byte_size, so.sha256
FROM calendar_event_attachments a
INNER JOIN storage_objects so ON so.id = a.storage_object_id
WHERE a.public_id = ? AND a.enabled = FALSE AND a.retired_at IS NULL;

-- CanReadObject says whether a user can already read the bytes of an object:
-- a live attachment on a live event, or a live album photo, holding it in a
-- calendar the user is a member of. A digest alone is not possession, so an
-- upload that would reuse stored bytes without sending them is allowed only
-- when this holds.
-- name: CanReadObject :one
SELECT EXISTS (
  SELECT 1 FROM calendar_event_attachments a
  INNER JOIN calendar_events e ON e.id = a.event_id AND e.enabled = TRUE
  INNER JOIN calendars c ON c.id = e.calendar_id AND c.enabled = TRUE
  INNER JOIN calendar_members cm
    ON cm.calendar_id = c.id AND cm.user_id = sqlc.arg(user_id) AND cm.enabled = TRUE
  WHERE a.storage_object_id = sqlc.arg(object_id) AND a.enabled = TRUE
) OR EXISTS (
  SELECT 1 FROM album_photos p
  INNER JOIN calendars c ON c.id = p.calendar_id AND c.enabled = TRUE
  INNER JOIN calendar_members cm
    ON cm.calendar_id = c.id AND cm.user_id = sqlc.arg(user_id) AND cm.enabled = TRUE
  WHERE (p.storage_object_id = sqlc.arg(photo_object_id) OR p.thumbnail_object_id = sqlc.arg(photo_object_id))
    AND p.enabled = TRUE
) AS readable;

-- CreateEventAttachment starts disabled. The row only becomes visible once
-- the upload has actually landed, so a presigned URL that is never used
-- leaves nothing pointing at an object that does not exist.
-- name: CreateEventAttachment :execresult
INSERT INTO calendar_event_attachments (public_id, workspace_id, event_id, uploader_id, storage_object_id, filename, enabled)
VALUES (?, ?, ?, ?, ?, ?, FALSE);

-- ConfirmEventAttachment enables a reservation only while its event and
-- calendar are live. The deletes of both retire live rows only, so a
-- reservation confirmed after its parent went would otherwise come alive
-- under it, holding a reference nothing will ever release. Reading the parents
-- in the same statement takes a shared lock on them, which orders this against
-- a delete running at the same time.
-- name: ConfirmEventAttachment :execresult
UPDATE calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
INNER JOIN calendars c ON c.id = e.calendar_id
SET a.enabled = TRUE
WHERE a.id = ? AND a.uploader_id = ? AND a.enabled = FALSE AND a.retired_at IS NULL
  AND e.enabled = TRUE AND c.enabled = TRUE;

-- SoftDeleteAttachment retires one live row. The enabled = TRUE guard makes
-- the affected-row count say whether this call is the one that retired it,
-- which is what decides whether it releases the reference.
-- name: SoftDeleteAttachment :execresult
UPDATE calendar_event_attachments SET enabled = FALSE, retired_at = CURRENT_TIMESTAMP(3)
WHERE id = ? AND enabled = TRUE;

-- name: DeletePendingAttachment :exec
DELETE FROM calendar_event_attachments
WHERE id = ? AND uploader_id = ? AND enabled = FALSE;

-- Only a confirmed row holds a reference: an unconfirmed reservation never
-- incremented one, and a row already soft-deleted released its own. Listing
-- either would decrement a count nobody took, and because objects are
-- content-addressed and shared, that drives a blob another calendar is still
-- using down to zero and into the sweep.
--
-- FOR UPDATE is what makes the listing the set of rows this transaction
-- retires. A concurrent delete of the same rows waits here, then reads them
-- as already retired, so each row is released once; a plain read would let
-- both list them and both release.
-- name: ListAttachmentObjectIDsByEvent :many
SELECT storage_object_id FROM calendar_event_attachments
WHERE event_id = ? AND enabled = TRUE
FOR UPDATE;

-- name: ListAttachmentObjectIDsByCalendar :many
SELECT a.storage_object_id
FROM calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
WHERE e.calendar_id = ? AND a.enabled = TRUE
FOR UPDATE;

-- name: SoftDeleteAttachmentsByEvent :exec
UPDATE calendar_event_attachments SET enabled = FALSE, retired_at = CURRENT_TIMESTAMP(3)
WHERE event_id = ? AND enabled = TRUE;

-- name: SoftDeleteAttachmentsByCalendar :exec
UPDATE calendar_event_attachments a
INNER JOIN calendar_events e ON e.id = a.event_id
SET a.enabled = FALSE, a.retired_at = CURRENT_TIMESTAMP(3)
WHERE e.calendar_id = ? AND a.enabled = TRUE;

-- DeleteAbandonedAttachment removes a reservation whose upload never
-- landed. It re-checks enabled = FALSE so a row confirmed between the
-- sweep listing it and this delete is left alone, and retired_at IS NULL so a
-- deleted attachment is kept for its own, longer retention.
-- name: DeleteAbandonedAttachment :exec
DELETE FROM calendar_event_attachments
WHERE id = ? AND enabled = FALSE AND retired_at IS NULL;

-- ListAbandonedAttachments walks by id rather than re-reading the head of the
-- table: a row the delete below cannot remove would otherwise be listed again
-- on the next pass, and the sweep would spend its whole batch budget failing
-- on the same page.
--
-- The public ids name the key a reservation may have uploaded its own copy to
-- (workspace/<workspace>/uploads/<attachment>), which nothing else tracks.
-- name: ListAbandonedAttachments :many
SELECT a.id, a.storage_object_id, a.public_id, w.public_id AS workspace_public_id
FROM calendar_event_attachments a
INNER JOIN workspaces w ON w.id = a.workspace_id
WHERE a.enabled = FALSE AND a.retired_at IS NULL AND a.created_at < ? AND a.id > ?
ORDER BY a.id
LIMIT ?;

-- ListRetiredAttachments walks soft-deleted rows whose retention has run out.
--
-- A soft-deleted row keeps the file's name and uploader for the activity
-- history, but it still points at the blob through a RESTRICT foreign key, so
-- the object cannot be collected while it exists. Removing the row after a
-- retention window is what finally lets the sweep reclaim the bytes; the
-- reference itself was released when the row was soft-deleted.
-- name: ListRetiredAttachments :many
SELECT a.id
FROM calendar_event_attachments a
WHERE a.enabled = FALSE AND a.retired_at IS NOT NULL AND a.updated_at < ? AND a.id > ?
ORDER BY a.id
LIMIT ?;

-- name: DeleteRetiredAttachment :exec
DELETE FROM calendar_event_attachments
WHERE id = ? AND enabled = FALSE AND retired_at IS NOT NULL;
