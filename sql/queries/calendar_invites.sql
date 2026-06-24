-- name: GetInviteByToken :one
SELECT * FROM calendar_invites
WHERE token = ? AND (expires_at IS NULL OR expires_at > NOW())
  AND (max_uses IS NULL OR use_count < max_uses);

-- name: CreateInvite :execresult
INSERT INTO calendar_invites (calendar_id, token, role, max_uses, expires_at, created_by)
VALUES (?, ?, ?, ?, ?, ?);

-- name: IncrementInviteUseCount :exec
UPDATE calendar_invites SET use_count = use_count + 1 WHERE id = ?;

-- name: ConsumeInviteUse :execresult
UPDATE calendar_invites SET use_count = use_count + 1
WHERE id = ? AND (max_uses IS NULL OR use_count < max_uses);

-- name: DeleteInvite :exec
DELETE FROM calendar_invites WHERE id = ?;

-- name: ListInvitesByCalendar :many
SELECT * FROM calendar_invites
WHERE calendar_id = ?
ORDER BY created_at DESC;

-- name: DeleteInviteByIDAndCalendar :exec
DELETE FROM calendar_invites WHERE id = ? AND calendar_id = ?;

-- name: GetInviteByTokenPublic :one
SELECT ci.*, c.public_id AS calendar_public_id, c.name AS calendar_name, c.color AS calendar_color
FROM calendar_invites ci
INNER JOIN calendars c ON c.id = ci.calendar_id
WHERE ci.token = ? AND (ci.expires_at IS NULL OR ci.expires_at > NOW())
  AND (ci.max_uses IS NULL OR ci.use_count < ci.max_uses);

-- name: ListEventsByInviteCalendar :many
SELECT e.* FROM events e
INNER JOIN calendar_invites ci ON ci.calendar_id = e.calendar_id
WHERE ci.token = ? AND e.recurrence_rule IS NULL AND e.recurrence_parent_id IS NULL
  AND e.start_at < ? AND e.end_at > ?
ORDER BY e.start_at;

-- name: ListRecurringEventsByInviteCalendar :many
SELECT e.* FROM events e
INNER JOIN calendar_invites ci ON ci.calendar_id = e.calendar_id
WHERE ci.token = ? AND e.recurrence_rule IS NOT NULL
  AND e.start_at < ? AND e.recurrence_end > ?
ORDER BY e.start_at;
