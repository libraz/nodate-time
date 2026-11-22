-- name: CreateDavAppPassword :execresult
INSERT INTO dav_app_passwords (public_id, workspace_id, user_id, name, token_hash, token_prefix)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListDavAppPasswords :many
SELECT * FROM dav_app_passwords
WHERE workspace_id = ? AND user_id = ? AND enabled = TRUE AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: CountLiveDavAppPasswords :one
SELECT COUNT(*) FROM dav_app_passwords
WHERE workspace_id = ? AND user_id = ? AND enabled = TRUE AND revoked_at IS NULL;

-- name: RevokeDavAppPassword :execresult
UPDATE dav_app_passwords
SET revoked_at = CURRENT_TIMESTAMP(3)
WHERE workspace_id = ? AND user_id = ? AND public_id = ? AND revoked_at IS NULL;

-- GetDavAppPasswordByHash authenticates a CalDAV request. It does not look at
-- identity locks: a lock throttles password guessing, and honouring it here
-- would let anyone who knows an address sign every device of that user out.
-- name: GetDavAppPasswordByHash :one
SELECT p.* FROM dav_app_passwords p
JOIN users u ON u.id = p.user_id AND u.enabled = TRUE
WHERE p.token_hash = ?
  AND p.enabled = TRUE
  AND p.revoked_at IS NULL
  AND EXISTS (
    SELECT 1 FROM identities ui
    WHERE ui.user_id = u.id AND ui.enabled = TRUE)
  AND NOT EXISTS (
    SELECT 1 FROM identities i
    WHERE i.user_id = u.id AND i.provider = 'local'
      AND i.enabled = TRUE AND i.mfa_confirmed_at IS NOT NULL);

-- GetDavAppPasswordByPublicIDAny reads one of a user's app passwords whatever
-- its state, so a caller can tell a revoked or used row from a missing one.
-- name: GetDavAppPasswordByPublicIDAny :one
SELECT enabled, revoked_at, last_used_at, token_hash FROM dav_app_passwords
WHERE public_id = ? AND workspace_id = ? AND user_id = ?;

-- TouchDavAppPassword records a use, at most once a minute per password.
-- name: TouchDavAppPassword :exec
UPDATE dav_app_passwords
SET last_used_at = CURRENT_TIMESTAMP(3)
WHERE id = ?
  AND (last_used_at IS NULL OR last_used_at < CURRENT_TIMESTAMP(3) - INTERVAL 1 MINUTE);

-- name: GetUserForAppPasswordCreate :one
SELECT
  u.email_verified_at,
  EXISTS (
    SELECT 1 FROM identities i
    WHERE i.user_id = u.id AND i.provider = 'local' AND i.enabled = TRUE
  ) AS has_local_identity,
  EXISTS (
    SELECT 1 FROM identities i
    WHERE i.user_id = u.id AND i.provider = 'local' AND i.enabled = TRUE
      AND i.mfa_confirmed_at IS NOT NULL
  ) AS has_local_mfa
FROM users u
WHERE u.id = ? AND u.enabled = TRUE;

-- name: InsertDavEventResource :exec
INSERT INTO dav_event_resources (event_id, href_name, uid)
VALUES (?, ?, ?);

-- ReassignDavEventResource moves a resource name from a soft-deleted event
-- to the event that replaces it.
-- name: ReassignDavEventResource :execresult
UPDATE dav_event_resources
SET event_id = sqlc.arg(new_event_id)
WHERE event_id = sqlc.arg(old_event_id);

-- The two lookups below return every match, oldest first, and leave the
-- enabled filter to the caller: a soft-deleted event's row is still needed to
-- name it as deleted and to reassign it.
-- name: GetDavEventResourcesByHref :many
SELECT r.* FROM dav_event_resources r
JOIN calendar_events e ON e.id = r.event_id
WHERE e.calendar_id = ? AND r.href_name = ?
ORDER BY r.created_at, r.event_id;

-- name: GetDavEventResourcesByUID :many
SELECT r.* FROM dav_event_resources r
JOIN calendar_events e ON e.id = r.event_id
WHERE e.calendar_id = ? AND r.uid = ?
ORDER BY r.created_at, r.event_id;

-- name: GetDavEventResourceByEvent :one
SELECT * FROM dav_event_resources WHERE event_id = ?;

-- name: ListDavEventResourcesByCalendar :many
SELECT r.* FROM dav_event_resources r
JOIN calendar_events e ON e.id = r.event_id
WHERE e.calendar_id = ?
ORDER BY r.created_at, r.event_id;

-- ListCalendarLogSince reads one calendar's slice of the event log in id
-- order: rows past min_id, plus rows at or after since that a slower
-- transaction committed below min_id. A member removal carries no calendar
-- and concerns every calendar of the workspace.
-- name: ListCalendarLogSince :many
SELECT * FROM events
WHERE workspace_id = ?
  AND (calendar_id = ? OR (calendar_id IS NULL AND type = 'workspace.member.removed'))
  AND (id > sqlc.arg(min_id) OR occurred_at >= sqlc.arg(since))
ORDER BY id
LIMIT ?;

-- name: GetMaxCalendarLogID :one
SELECT CAST(COALESCE(MAX(id), 0) AS UNSIGNED) FROM events
WHERE workspace_id = ?
  AND (calendar_id = ? OR (calendar_id IS NULL AND type = 'workspace.member.removed'));

-- name: GetCalendarLogRow :one
SELECT * FROM events WHERE id = ?;

-- name: GetCalendarEventByPublicIDAny :one
SELECT * FROM calendar_events
WHERE workspace_id = ? AND public_id = ?;

-- name: ListRecurrenceOverridesByParentAny :many
SELECT * FROM calendar_events
WHERE recurrence_parent_id = ?
ORDER BY recurrence_original_start;
