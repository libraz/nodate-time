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

-- name: RecordFailedLogin :exec
UPDATE identities
SET failed_attempts = failed_attempts + 1, locked_until_at = ?
WHERE id = ?;

-- name: RecordSuccessfulLogin :exec
UPDATE identities
SET failed_attempts = 0, locked_until_at = NULL, last_used_at = NOW(3)
WHERE id = ?;

-- name: DisableIdentity :exec
UPDATE identities SET enabled = FALSE WHERE user_id = ? AND provider = ?;
