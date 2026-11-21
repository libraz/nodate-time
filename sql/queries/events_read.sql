-- Reads over the event log and the calendar rows it points at, for the
-- activity feed and per-event history.

-- ListCalendarLogPage is one page of the calendar's log, newest first, keyed
-- by id alone (see ListEventsByCalendar for why). before_id is the exclusive
-- upper bound; the first page passes the largest id.
-- name: ListCalendarLogPage :many
SELECT e.id, e.public_id, e.subject_public_id, e.type, e.payload_json, e.occurred_at,
       e.actor_system_source,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url,
       so.storage_key AS actor_avatar_storage_key
FROM events e FORCE INDEX (idx_events_workspace_calendar_id)
LEFT JOIN users u ON u.id = e.actor_user_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND e.id < sqlc.arg(before_id)
ORDER BY e.id DESC
LIMIT ?;

-- ListSubjectLogPage is one page of the log rows about a single entity,
-- newest first.
-- name: ListSubjectLogPage :many
SELECT e.id, e.public_id, e.subject_public_id, e.type, e.payload_json, e.occurred_at,
       e.actor_system_source,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url,
       so.storage_key AS actor_avatar_storage_key
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND e.subject_public_id = sqlc.arg(subject_id)
  AND e.id < sqlc.arg(before_id)
ORDER BY e.id DESC
LIMIT ?;

-- ListLogSubjectEvents reads the calendar rows a page of log entries is
-- about, soft-deleted ones included: a deleted event keeps its history.
-- name: ListLogSubjectEvents :many
SELECT id, public_id, visibility, owner_user_id, created_by_user_id
FROM calendar_events
WHERE workspace_id = ?
  AND calendar_id = ?
  AND public_id IN (sqlc.slice('public_ids'));

-- ListLogOverrides reads the live changed occurrences of the given series,
-- which carry their own participants.
-- name: ListLogOverrides :many
SELECT id, recurrence_parent_id, recurrence_original_start, visibility,
       owner_user_id, created_by_user_id
FROM calendar_events
WHERE recurrence_parent_id IN (sqlc.slice('parent_ids')) AND enabled = TRUE;

-- ListLogAttendees returns every attendee row of the given events, removed
-- ones included: an override that has any row replaces the series' list.
-- name: ListLogAttendees :many
SELECT event_id, user_id, enabled
FROM calendar_event_attendees
WHERE event_id IN (sqlc.slice('event_ids'));
