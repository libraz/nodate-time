-- name: CreatePasswordReset :execresult
INSERT INTO password_resets (user_id, token_hash, expires_at)
VALUES (?, ?, ?);

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

-- name: DeleteExpiredPasswordResets :exec
DELETE FROM password_resets WHERE expires_at < ? OR used_at IS NOT NULL;
