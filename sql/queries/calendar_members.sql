-- name: GetCalendarMember :one
SELECT * FROM calendar_members
WHERE calendar_id = ? AND user_id = ? AND enabled = TRUE;

-- name: GetCalendarMemberForUpdate :one
SELECT * FROM calendar_members
WHERE calendar_id = ? AND user_id = ? AND enabled = TRUE FOR UPDATE;

-- name: ListCalendarMembers :many
SELECT cm.*, u.public_id AS user_public_id, u.display_name AS user_display_name,
       u.email AS user_email, u.avatar_url AS user_avatar_url
FROM calendar_members cm
INNER JOIN users u ON u.id = cm.user_id
WHERE cm.calendar_id = ? AND cm.enabled = TRUE
ORDER BY cm.created_at;

-- AddCalendarMember revives a revoked grant rather than inserting beside
-- it: the unique key spans revoked rows precisely so a re-add cannot leave
-- an older grant behind for an access check to find.
-- name: AddCalendarMember :execresult
INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role, member_color, invited_by_user_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  role = VALUES(role),
  member_color = VALUES(member_color),
  invited_by_user_id = VALUES(invited_by_user_id),
  enabled = TRUE;

-- name: UpdateCalendarMemberRole :exec
UPDATE calendar_members SET role = ? WHERE calendar_id = ? AND user_id = ?;

-- name: UpdateCalendarMemberColor :exec
UPDATE calendar_members SET member_color = ? WHERE calendar_id = ? AND user_id = ?;

-- name: RevokeCalendarMember :exec
UPDATE calendar_members SET enabled = FALSE WHERE calendar_id = ? AND user_id = ?;

-- CountCalendarOwners guards the last-owner case. Roles above editor can
-- change membership, so losing the final owner would leave a calendar
-- nobody can administer.
-- name: CountCalendarOwners :one
SELECT COUNT(*) FROM calendar_members
WHERE calendar_id = ? AND role = 'owner' AND enabled = TRUE;

-- name: CountCalendarOwnersForUpdate :one
SELECT COUNT(*) FROM calendar_members
WHERE calendar_id = ? AND role = 'owner' AND enabled = TRUE FOR UPDATE;
