-- Authentication identities. One row per (provider, subject); the local
-- provider's subject is the user's email at signup time.

-- name: GetIdentityByProviderSubject :one
SELECT * FROM identities
WHERE provider = ? AND subject = ? AND enabled = TRUE;

-- name: GetLocalIdentityByUser :one
SELECT * FROM identities
WHERE user_id = ? AND provider = 'local' AND enabled = TRUE;

-- name: GetLocalIdentityByUserForUpdate :one
SELECT * FROM identities
WHERE user_id = ? AND provider = 'local' AND enabled = TRUE FOR UPDATE;

-- name: ListIdentitiesForUser :many
SELECT * FROM identities WHERE user_id = ? AND enabled = TRUE ORDER BY created_at;

-- name: CreateIdentity :execresult
INSERT INTO identities (public_id, user_id, provider, subject, password_hash)
VALUES (?, ?, ?, ?, ?);

-- UpdatePasswordHash also clears the lockout counters: whoever completes a
-- reset has demonstrated control of the account, so carrying the previous
-- attempt count forward would lock them straight back out.
-- name: UpdatePasswordHash :exec
UPDATE identities
SET password_hash = ?, failed_attempts = 0, locked_until_at = NULL
WHERE user_id = ? AND provider = 'local';

-- RecordFailedLogin counts consecutive failures since the last success or the
-- last lock running out. Nothing records when each failure happened, so ones
-- spread over any length of time accumulate until they reach the threshold.
-- A failure arriving after an expired lock starts a new count of one: a
-- counter that never forgets would re-lock on a single guess, forever.
--
-- Counting and locking are one statement so that a single clock answers both.
-- Deciding the lock in Go against time.Now() while the reset here is decided
-- against NOW(3) lets the two disagree by a millisecond at the boundary, and
-- the row then holds a count of one alongside a fifteen-minute lock.
--
-- The assignments are order-dependent: MySQL evaluates SET left to right, so
-- locked_until_at is compared against the count assigned on the line above it,
-- not the one the row arrived with. Reversing the two lines locks one attempt
-- late and then decides the reset from a locked_until_at this same statement
-- has already overwritten.
-- name: RecordFailedLogin :exec
UPDATE identities
SET failed_attempts = IF(locked_until_at IS NOT NULL AND locked_until_at <= NOW(3), 1, failed_attempts + 1),
    locked_until_at = IF(failed_attempts >= sqlc.arg(lockout_threshold),
                         NOW(3) + INTERVAL sqlc.arg(lockout_window_minutes) MINUTE, NULL)
WHERE id = sqlc.arg(id);

-- name: RecordSuccessfulLogin :exec
UPDATE identities
SET failed_attempts = 0, locked_until_at = NULL, last_used_at = NOW(3)
WHERE id = ?;

-- name: DisableIdentity :exec
UPDATE identities SET enabled = FALSE WHERE user_id = ? AND provider = ?;
