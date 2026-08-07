-- name: CreatePasswordReset :execresult
INSERT INTO password_resets (public_id, user_id, token_hash, expires_at)
VALUES (?, ?, ?, ?);

-- name: GetPasswordResetByTokenHash :one
SELECT * FROM password_resets
WHERE token_hash = ?
  AND used_at IS NULL
  AND expires_at > CURRENT_TIMESTAMP(3)
LIMIT 1
FOR UPDATE;

-- name: MarkPasswordResetUsed :execresult
UPDATE password_resets
SET used_at = CURRENT_TIMESTAMP(3)
WHERE id = ? AND used_at IS NULL;

-- name: InvalidateUserPasswordResets :exec
UPDATE password_resets SET used_at = CURRENT_TIMESTAMP(3)
WHERE user_id = ? AND used_at IS NULL;

-- DeleteExpiredPasswordResets removes one batch of tokens past their expiry
-- and reports how many, so the caller can loop until the backlog is drained.
--
-- Spent tokens are not collected early any more. `used_at IS NOT NULL` cannot
-- be answered from an expiry index, so keeping it in the same statement meant
-- the whole table was read on every tick to save a redeemed row an hour of
-- shelf life -- and the row is already inert, because
-- GetPasswordResetByTokenHash refuses a token with a redemption time. It is
-- collected on its own expiry along with everything else.
-- name: DeleteExpiredPasswordResets :execresult
DELETE FROM password_resets WHERE expires_at < ? ORDER BY expires_at LIMIT ?;
