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

-- RotateSessionByHash exchanges one refresh token for the next in a single
-- statement, so two requests arriving with the same token cannot both come
-- away with credentials: whichever reaches the row first replaces the hash
-- they matched on, and the other matches nothing. Reading the row and then
-- updating it would let both through, and the loser's tokens would stop
-- working with no record of why.
--
-- The assignments are order-dependent: MySQL evaluates SET left to right, so
-- prev_refresh_hash takes the old value only because it is assigned before
-- refresh_hash is overwritten.
-- name: RotateSessionByHash :execresult
UPDATE sessions
SET prev_refresh_hash = refresh_hash,
    refresh_hash = ?,
    expires_at = ?,
    rotated_at = NOW(3),
    last_used_at = NOW(3)
WHERE refresh_hash = ?
  AND revoked_at IS NULL
  AND enabled = TRUE
  AND expires_at > NOW(3);

-- GetSessionByPrevRefreshHash finds the session a spent refresh token used to
-- belong to. Matching here is the only evidence that a token which opens no
-- session was ever real, which is what separates a replay from a guess.
-- name: GetSessionByPrevRefreshHash :one
SELECT * FROM sessions
WHERE prev_refresh_hash = ?
  AND revoked_at IS NULL
  AND enabled = TRUE;

-- RevokeReplayedSession ends a session whose retired refresh token has been
-- presented a second time -- unless the exchange that retired it was seconds
-- ago, in which case this is one client asking twice rather than two parties
-- holding the same token.
--
-- A browser resuming with several expired requests sends a refresh for each.
-- Only one can win, and without the interval below every loser would end the
-- session, so waking a sleeping tab would sign the reader out. That cost is
-- certain; a thief landing inside the same few seconds is not. The losers are
-- refused either way -- the window decides whether a refusal also destroys
-- the session, not whether it grants anything.
--
-- The comparison is made here rather than in the caller so that one clock
-- decides it. Reading rotated_at back and comparing it against the
-- application's own clock would let a few seconds of skew revoke the very
-- sessions this is meant to protect.
-- name: RevokeReplayedSession :execresult
UPDATE sessions
SET revoked_at = NOW(3)
WHERE id = ?
  AND revoked_at IS NULL
  AND (rotated_at IS NULL OR rotated_at < NOW(3) - INTERVAL 10 SECOND);

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

-- DeleteExpiredSessions removes one batch and reports how many it took, so
-- the caller can loop until a short batch says the backlog is drained.
--
-- The bound is what keeps a single statement from locking every expired row
-- at once: this table grows with every sign-in on the deployment, and the
-- sweep that collects it must not hold the table for as long as the backlog
-- happens to be. Ordering by expiry makes each batch the oldest rows, so a
-- run that is cut short still leaves the newest -- and enters the expiry
-- index rather than sorting what it read.
-- name: DeleteExpiredSessions :execresult
DELETE FROM sessions WHERE expires_at < ? ORDER BY expires_at LIMIT ?;
