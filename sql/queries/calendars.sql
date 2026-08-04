-- name: GetCalendarByPublicID :one
SELECT * FROM calendars WHERE workspace_id = ? AND public_id = ? AND enabled = TRUE;

-- name: GetCalendarByID :one
SELECT * FROM calendars WHERE id = ? AND enabled = TRUE;

-- name: GetCalendarByIDForUpdate :one
SELECT * FROM calendars WHERE id = ? FOR UPDATE;

-- ListCalendarsByUser joins through calendar_members, which is the access
-- grant. A calendar the user cannot reach must not appear, so this is the
-- same table every authorization check resolves through.
-- name: ListCalendarsByUser :many
SELECT c.*, cm.role AS member_role, cm.member_color
FROM calendars c
INNER JOIN calendar_members cm ON cm.calendar_id = c.id AND cm.enabled = TRUE
WHERE cm.user_id = ? AND c.workspace_id = ? AND c.enabled = TRUE
ORDER BY c.created_at DESC;

-- CreateCalendar leaves owner_user_id NULL: these are calendars a group
-- shares, and the owner key cascades, so naming one would mean that
-- person's removal deletes everyone else's history.
-- name: CreateCalendar :execresult
INSERT INTO calendars (public_id, workspace_id, kind, name, color)
VALUES (?, ?, 'personal', ?, ?);

-- name: UpdateCalendar :exec
UPDATE calendars SET name = ?, color = ?, cover_url = ? WHERE id = ?;

-- name: SoftDeleteCalendar :exec
UPDATE calendars SET enabled = FALSE WHERE id = ?;
