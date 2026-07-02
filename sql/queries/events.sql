-- name: GetEventByPublicID :one
SELECT * FROM events WHERE public_id = ?;

-- name: GetEventByID :one
SELECT * FROM events WHERE id = ?;

-- name: ListEventsByCalendarAndRange :many
SELECT * FROM events
WHERE calendar_id = ? AND recurrence_rule IS NULL AND recurrence_parent_id IS NULL
  AND start_at < ? AND end_at > ?
ORDER BY start_at;

-- name: ListRecurringEventsByCalendarAndRange :many
SELECT * FROM events
WHERE calendar_id = ? AND recurrence_rule IS NOT NULL AND recurrence_parent_id IS NULL
  AND start_at < ? AND recurrence_end > ?
ORDER BY start_at;

-- name: ListRecurrenceExceptionsByParent :many
SELECT * FROM events WHERE recurrence_parent_id = ? ORDER BY recurrence_original_start;

-- name: GetRecurrenceException :one
SELECT * FROM events
WHERE recurrence_parent_id = ? AND recurrence_original_start = ?;

-- name: UpsertRecurrenceException :execresult
INSERT INTO events (
  public_id, calendar_id, title, all_day, start_at, end_at, timezone, color,
  location, memo, url, created_by, assigned_to, notification_offset,
  recurrence_parent_id, recurrence_original_start, recurrence_cancelled
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  title = VALUES(title), all_day = VALUES(all_day), start_at = VALUES(start_at),
  end_at = VALUES(end_at), timezone = VALUES(timezone), color = VALUES(color),
  location = VALUES(location), memo = VALUES(memo), url = VALUES(url),
  assigned_to = VALUES(assigned_to), notification_offset = VALUES(notification_offset),
  recurrence_cancelled = VALUES(recurrence_cancelled);

-- name: CreateEvent :execresult
INSERT INTO events (public_id, calendar_id, title, all_day, start_at, end_at, timezone, color, location, memo, url, created_by, assigned_to, notification_offset, recurrence_rule, recurrence_end)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateEvent :exec
UPDATE events SET title = ?, all_day = ?, start_at = ?, end_at = ?, timezone = ?, color = ?, location = ?, memo = ?, url = ?, assigned_to = ?, notification_offset = ?, recurrence_rule = ?, recurrence_end = ?
WHERE id = ?;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = ?;
