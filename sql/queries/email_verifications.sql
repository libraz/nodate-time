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

-- DeleteExpiredEmailVerifications removes one batch of tokens past their
-- expiry and reports how many, so the caller can loop until the backlog is
-- drained.
--
-- A redeemed token is collected on its own expiry like the rest: used_at
-- cannot be answered from the expiry index, and GetEmailVerificationByTokenHash
-- already refuses a token with a redemption time.
-- name: DeleteExpiredEmailVerifications :execresult
DELETE FROM email_verifications WHERE expires_at < ? ORDER BY expires_at LIMIT ?;
