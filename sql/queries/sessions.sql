-- Refresh-token sessions. Only the hash is stored, so a database read
-- cannot yield a usable token.

-- name: CreateSession :execresult
INSERT INTO sessions (public_id, user_id, refresh_hash, user_agent, ip_address, expires_at)
VALUES (?, ?, ?, ?, ?, ?);

-- GetLiveSession is the per-request check behind an access token. The
-- token carries the session id, so revoking one device signs out that
-- device and no other -- which a version counter on the user row cannot
-- express, since it can only invalidate all of them at once.
--
-- The token names the session by its public id. The internal ids are one
-- deployment-wide sequence, and an access token is a value a client holds.
-- name: GetLiveSession :one
SELECT * FROM sessions
WHERE public_id = ?
  AND revoked_at IS NULL
  AND enabled = TRUE
  AND expires_at > NOW(3);

-- name: GetSessionByRefreshHash :one
SELECT * FROM sessions
WHERE refresh_hash = ?
  AND revoked_at IS NULL
  AND enabled = TRUE
  AND expires_at > NOW(3);

-- name: RotateSession :exec
UPDATE sessions
SET refresh_hash = ?, expires_at = ?, last_used_at = NOW(3)
WHERE id = ?;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = NOW(3) WHERE id = ?;

-- RevokeAllUserSessions is what a password change calls. Replacing the
-- credential has to invalidate every session opened with the old one, or
-- whoever knew it keeps their access.
-- name: RevokeAllUserSessions :exec
UPDATE sessions SET revoked_at = NOW(3) WHERE user_id = ? AND revoked_at IS NULL;

-- name: ListSessionsForUser :many
SELECT * FROM sessions
WHERE user_id = ? AND revoked_at IS NULL AND expires_at > NOW(3)
ORDER BY created_at DESC;

-- RevokeSessionByPublicID is scoped to the owning user: a session is named by
-- a value its holder was given, and one person's list must not reach another's
-- devices.
-- name: RevokeSessionByPublicID :execresult
UPDATE sessions SET revoked_at = NOW(3)
WHERE public_id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ?;
