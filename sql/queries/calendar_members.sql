-- name: GetCalendarMember :one
SELECT * FROM calendar_members
WHERE calendar_id = ? AND user_id = ? AND enabled = TRUE;

-- name: GetCalendarMemberForUpdate :one
SELECT * FROM calendar_members
WHERE calendar_id = ? AND user_id = ? AND enabled = TRUE FOR UPDATE;

-- The LIMIT is a cap rather than a page. Membership is a list of people, and
-- every caller of this -- the member sheet, the participant picker, the
-- colour legend -- wants all of them at once; paging it would mean a picker
-- that cannot offer somebody who is in the calendar. The cap is a ceiling on
-- what one response can cost, not a page size.
--
-- The avatar object is joined in rather than looked up per member: a picture
-- is a URL only once it is signed, and asking for the key one member at a time
-- makes the sheet cost a query per person.
-- name: ListCalendarMembers :many
SELECT cm.*, u.public_id AS user_public_id, u.display_name AS user_display_name,
       u.email AS user_email, u.avatar_url AS user_avatar_url,
       so.storage_key AS user_avatar_storage_key
FROM calendar_members cm
INNER JOIN users u ON u.id = cm.user_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE cm.calendar_id = ? AND cm.enabled = TRUE
ORDER BY cm.created_at
LIMIT ?;

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
