-- name: GetCalendarByPublicID :one
SELECT * FROM calendars WHERE public_id = ?;

-- name: GetCalendarByID :one
SELECT * FROM calendars WHERE id = ?;

-- name: ListCalendarsByUser :many
SELECT c.* FROM calendars c
INNER JOIN calendar_members cm ON cm.calendar_id = c.id
WHERE cm.user_id = ?
ORDER BY c.created_at DESC;

-- name: CreateCalendar :execresult
INSERT INTO calendars (public_id, name, color, created_by)
VALUES (?, ?, ?, ?);

-- name: UpdateCalendar :exec
UPDATE calendars SET name = ?, color = ?, cover_url = ? WHERE id = ?;

-- name: DeleteCalendar :exec
DELETE FROM calendars WHERE id = ?;
