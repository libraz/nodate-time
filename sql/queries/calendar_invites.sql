-- Share links. Tokens are stored hashed, so the plaintext exists only in
-- the link the creator was shown once.

-- name: GetInviteByTokenHash :one
SELECT * FROM calendar_invites
WHERE token_hash = ?
  AND enabled = TRUE
  AND (expires_at IS NULL OR expires_at > NOW(3))
  AND (max_uses IS NULL OR use_count < max_uses);

-- name: GetInviteByTokenHashWithCalendar :one
SELECT ci.*, c.public_id AS calendar_public_id, c.name AS calendar_name, c.color AS calendar_color
FROM calendar_invites ci
INNER JOIN calendars c ON c.id = ci.calendar_id AND c.enabled = TRUE
WHERE ci.token_hash = ?
  AND ci.enabled = TRUE
  AND (ci.expires_at IS NULL OR ci.expires_at > NOW(3));

-- name: CreateInvite :execresult
INSERT INTO calendar_invites (public_id, workspace_id, calendar_id, created_by_user_id, token_hash, role, max_uses, expires_at, is_public)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- ConsumeInviteUse increments and reports whether it did. The limit is
-- checked in the UPDATE rather than by reading first, so two concurrent
-- acceptances of a single-use link cannot both pass.
-- name: ConsumeInviteUse :execresult
UPDATE calendar_invites SET use_count = use_count + 1
WHERE id = ? AND enabled = TRUE AND (max_uses IS NULL OR use_count < max_uses);

-- name: RevokeInvite :exec
UPDATE calendar_invites SET enabled = FALSE WHERE id = ?;

-- name: RevokeInviteByIDAndCalendar :execresult
UPDATE calendar_invites SET enabled = FALSE WHERE id = ? AND calendar_id = ?;

-- name: ListInvitesByCalendar :many
SELECT * FROM calendar_invites
WHERE calendar_id = ? AND enabled = TRUE
ORDER BY created_at DESC;

-- name: ListPublicSharedCalendarIDs :many
SELECT DISTINCT ci.calendar_id
FROM calendar_invites ci
INNER JOIN calendar_members cm ON cm.calendar_id = ci.calendar_id AND cm.enabled = TRUE
WHERE cm.user_id = ?
  AND ci.is_public = TRUE
  AND ci.enabled = TRUE
  AND (ci.expires_at IS NULL OR ci.expires_at > NOW(3));

-- name: CountActivePublicInvites :one
SELECT COUNT(*) FROM calendar_invites
WHERE calendar_id = ?
  AND is_public = TRUE
  AND enabled = TRUE
  AND (expires_at IS NULL OR expires_at > NOW(3));
