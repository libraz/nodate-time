-- name: CreateEmailVerification :execresult
INSERT INTO email_verifications (public_id, user_id, email, token_hash, expires_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetEmailVerificationByTokenHash :one
SELECT * FROM email_verifications
WHERE token_hash = ?
  AND used_at IS NULL
  AND expires_at > CURRENT_TIMESTAMP(3)
LIMIT 1
FOR UPDATE;

-- name: MarkEmailVerificationUsed :execresult
UPDATE email_verifications
SET used_at = CURRENT_TIMESTAMP(3)
WHERE id = ? AND used_at IS NULL;

-- name: InvalidateUserEmailVerifications :exec
UPDATE email_verifications SET used_at = CURRENT_TIMESTAMP(3)
WHERE user_id = ? AND used_at IS NULL;

-- name: DeleteExpiredEmailVerifications :exec
DELETE FROM email_verifications WHERE expires_at < ? OR used_at IS NOT NULL;
