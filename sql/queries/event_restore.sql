-- Restoring a soft-deleted event.

-- GetCalendarEventAnyStateByPublicID reads an event whether or not it is
-- deleted, so a restore can tell a deleted event from a missing one.
-- name: GetCalendarEventAnyStateByPublicID :one
SELECT * FROM calendar_events WHERE workspace_id = ? AND public_id = ?;

-- name: GetCalendarEventAnyStateForUpdate :one
SELECT * FROM calendar_events WHERE id = ? FOR UPDATE;

-- FindRestorableDeletion is the deletion a restore undoes: the newest
-- whole-event delete of the subject (a single-occurrence cancellation carries
-- a scope and is not one) that no restore has followed.
-- name: FindRestorableDeletion :one
SELECT e.id, e.occurred_at, e.payload_json
FROM events e
WHERE e.workspace_id = sqlc.arg(workspace_id)
  AND e.calendar_id = sqlc.arg(calendar_id)
  AND e.subject_public_id = sqlc.arg(subject_id)
  AND e.type = 'calendar.event.deleted'
  AND JSON_EXTRACT(e.payload_json, '$.scope') IS NULL
  AND e.id > COALESCE((
    SELECT MAX(r.id) FROM events r
    WHERE r.workspace_id = sqlc.arg(workspace_id)
      AND r.calendar_id = sqlc.arg(calendar_id)
      AND r.subject_public_id = sqlc.arg(subject_id)
      AND r.type = 'calendar.event.restored'
  ), 0)
ORDER BY e.id DESC
LIMIT 1;

-- name: RestoreCalendarEvent :exec
UPDATE calendar_events SET enabled = TRUE WHERE id = ? AND task_id IS NULL;

-- RestoreAttachmentsByPublicID brings back only attachments the event's
-- deletion retired: a pending upload has no retirement time, and one removed
-- before the event was has its own, earlier row in the log.
-- name: RestoreAttachmentsByPublicID :exec
UPDATE calendar_event_attachments SET enabled = TRUE, retired_at = NULL
WHERE event_id = ? AND enabled = FALSE AND retired_at IS NOT NULL
  AND public_id IN (sqlc.slice(public_ids));
