-- Calendar events.
--
-- Two things changed with the shared contract and both show up here.
--
-- The table is calendar_events; `events` is now the append-only log.
--
-- A cancelled occurrence is an entry in the parent's recurrence_exceptions,
-- not a row. Only a *changed* occurrence produces a row, and it names the
-- occurrence it replaces in recurrence_original_start. The contract allows
-- exactly one representation per outcome, so nothing here writes both.

-- name: GetCalendarEventByPublicID :one
SELECT * FROM calendar_events
WHERE workspace_id = ? AND public_id = ? AND enabled = TRUE;

-- name: GetCalendarEventByID :one
SELECT * FROM calendar_events WHERE id = ? AND enabled = TRUE;

-- name: ListCalendarEventsByCalendarAndRange :many
SELECT * FROM calendar_events
WHERE calendar_id = ?
  AND enabled = TRUE
  AND recurrence_rule IS NULL
  AND recurrence_parent_id IS NULL
  AND start_at < sqlc.arg(range_end)
  AND end_at > sqlc.arg(range_start)
ORDER BY start_at;

-- name: ListRecurringCalendarEventsByCalendarAndRange :many
SELECT * FROM calendar_events
WHERE calendar_id = ?
  AND enabled = TRUE
  AND recurrence_rule IS NOT NULL
  AND recurrence_parent_id IS NULL
  AND start_at < sqlc.arg(range_end)
  AND (recurrence_end IS NULL OR recurrence_end > sqlc.arg(range_start))
ORDER BY start_at;

-- ListCalendarEventsForExport returns every series head in a calendar, dated or
-- not, recurring or not. Overrides are excluded: they belong to the series
-- that owns them and are read through it.
--
-- There is deliberately no window. An export is a backup, and a backup that
-- quietly stops at a boundary is worse than no backup -- the caller cannot
-- tell the difference between "the calendar had nothing there" and "the
-- server decided not to look". Callers that want a window apply it to the
-- result themselves and say so.
-- name: ListCalendarEventsForExport :many
SELECT * FROM calendar_events
WHERE calendar_id = ?
  AND enabled = TRUE
  AND recurrence_parent_id IS NULL
ORDER BY start_at, id;

-- name: ListRecurrenceOverridesByParent :many
SELECT * FROM calendar_events
WHERE recurrence_parent_id = ? AND enabled = TRUE
ORDER BY recurrence_original_start;

-- name: GetRecurrenceOverride :one
SELECT * FROM calendar_events
WHERE recurrence_parent_id = ? AND recurrence_original_start = ? AND enabled = TRUE;

-- name: UpsertRecurrenceOverride :execresult
INSERT INTO calendar_events (
  public_id, workspace_id, calendar_id, kind, visibility, show_as, flexibility,
  title, all_day, start_at, end_at, timezone, location, memo, url,
  owner_user_id, created_by_user_id, notification_offset,
  recurrence_parent_id, recurrence_original_start
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  kind = VALUES(kind),
  visibility = VALUES(visibility),
  show_as = VALUES(show_as),
  flexibility = VALUES(flexibility),
  title = VALUES(title),
  all_day = VALUES(all_day),
  start_at = VALUES(start_at),
  end_at = VALUES(end_at),
  timezone = VALUES(timezone),
  location = VALUES(location),
  memo = VALUES(memo),
  url = VALUES(url),
  owner_user_id = VALUES(owner_user_id),
  notification_offset = VALUES(notification_offset),
  enabled = TRUE;

-- name: DeleteRecurrenceOverridesByParent :exec
DELETE FROM calendar_events WHERE recurrence_parent_id = ?;

-- RetimeRecurrenceOverride moves one override row with the series it belongs
-- to. The original start moves with it: it identifies which occurrence is
-- replaced, so leaving it behind would orphan the override against a parent
-- that no longer generates that occurrence.
--
-- The instants are computed by the caller rather than added here. Occurrences
-- step in calendar units in the event's own timezone, so a series that moves
-- across a DST boundary moves by a different number of hours than of days, and
-- an interval added in SQL lands the override off the grid it names.
-- name: RetimeRecurrenceOverride :exec
UPDATE calendar_events
SET recurrence_original_start = ?, start_at = ?, end_at = ?
WHERE id = ?;

-- ExtendRecurrenceEnd widens a series' end boundary to cover an occurrence
-- that was moved past it.
--
-- recurrence_end is what range queries filter masters on, and a moved
-- occurrence is a row hanging off the master rather than one the rule
-- produces. Left alone, the master stops being selected for the window the
-- occurrence now falls in, so the row survives but appears in no view,
-- export or share -- indistinguishable from a deletion.
-- name: ExtendRecurrenceEnd :exec
UPDATE calendar_events
SET recurrence_end = sqlc.arg(recurrence_end)
WHERE id = sqlc.arg(id)
  AND recurrence_end IS NOT NULL
  AND recurrence_end < sqlc.arg(boundary);

-- name: CreateCalendarEvent :execresult
INSERT INTO calendar_events (
  public_id, workspace_id, calendar_id, kind, visibility, show_as, flexibility,
  title, all_day, start_at, end_at, timezone, location, memo, url,
  owner_user_id, created_by_user_id, block_label, notification_offset,
  recurrence_rule, recurrence_end
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateCalendarEvent :exec
UPDATE calendar_events
SET kind = ?, visibility = ?, show_as = ?, flexibility = ?,
    title = ?, all_day = ?, start_at = ?, end_at = ?, timezone = ?,
    location = ?, memo = ?, url = ?, owner_user_id = ?, block_label = ?,
    notification_offset = ?, recurrence_rule = ?, recurrence_end = ?
WHERE id = ?;

-- SetRecurrenceExceptions replaces the whole exclusion list. Cancelling one
-- occurrence is a read-modify-write of this column inside the caller's
-- transaction, not a tombstone row.
-- name: SetRecurrenceExceptions :exec
UPDATE calendar_events SET recurrence_exceptions = ? WHERE id = ?;

-- name: SoftDeleteCalendarEvent :exec
UPDATE calendar_events SET enabled = FALSE WHERE id = ?;

-- A reminder is delivered by whatever client holds the calendar, as the
-- iCalendar alarm the export writes. Nothing here dispatches one, so the
-- queries that would drive a dispatcher are absent rather than sitting unused
-- and reading as a delivery path that exists.
--
-- notification_offset is stored per event; a per-occurrence sent/not-sent
-- record is what a dispatcher would have to add first, since a series has one
-- row and many occurrences to remind about.
