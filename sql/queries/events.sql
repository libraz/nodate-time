-- name: GetEventByPublicID :one
SELECT * FROM events WHERE public_id = ?;

-- name: ListEventsByCalendarAndRange :many
SELECT * FROM events
WHERE calendar_id = ? AND recurrence_rule IS NULL AND start_at < ? AND end_at > ?
ORDER BY start_at;

-- name: ListRecurringEventsByCalendarAndRange :many
SELECT * FROM events
WHERE calendar_id = ? AND recurrence_rule IS NOT NULL
  AND start_at < ? AND recurrence_end > ?
ORDER BY start_at;

-- name: ListEventsByUserAndRange :many
SELECT e.* FROM events e
INNER JOIN calendar_members cm ON cm.calendar_id = e.calendar_id
WHERE cm.user_id = ? AND e.start_at < ? AND e.end_at > ?
ORDER BY e.start_at;

-- name: CreateEvent :execresult
INSERT INTO events (public_id, calendar_id, title, all_day, start_at, end_at, color, location, memo, url, created_by, assigned_to, notification_offset, recurrence_rule, recurrence_end)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateEvent :exec
UPDATE events SET title = ?, all_day = ?, start_at = ?, end_at = ?, color = ?, location = ?, memo = ?, url = ?, assigned_to = ?, notification_offset = ?, recurrence_rule = ?, recurrence_end = ?
WHERE id = ?;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = ?;
