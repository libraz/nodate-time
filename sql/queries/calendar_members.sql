-- name: GetCalendarMember :one
SELECT * FROM calendar_members WHERE calendar_id = ? AND user_id = ?;

-- name: ListCalendarMembers :many
SELECT cm.*, u.public_id AS user_public_id, u.name AS user_name, u.email AS user_email, u.icon AS user_icon
FROM calendar_members cm
INNER JOIN users u ON u.id = cm.user_id
WHERE cm.calendar_id = ?
ORDER BY cm.joined_at;

-- name: AddCalendarMember :execresult
INSERT INTO calendar_members (calendar_id, user_id, role, color)
VALUES (?, ?, ?, ?);

-- name: UpdateCalendarMemberRole :exec
UPDATE calendar_members SET role = ? WHERE calendar_id = ? AND user_id = ?;

-- name: RemoveCalendarMember :exec
DELETE FROM calendar_members WHERE calendar_id = ? AND user_id = ?;

-- name: CountCalendarAdmins :one
SELECT COUNT(*) FROM calendar_members WHERE calendar_id = ? AND role = 'admin';

-- name: CountCalendarAdminsForUpdate :one
SELECT COUNT(*) FROM calendar_members WHERE calendar_id = ? AND role = 'admin' FOR UPDATE;

-- name: GetCalendarMemberForUpdate :one
SELECT * FROM calendar_members WHERE calendar_id = ? AND user_id = ? FOR UPDATE;
