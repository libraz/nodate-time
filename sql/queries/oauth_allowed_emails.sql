-- name: ListAllowedEmails :many
SELECT * FROM oauth_allowed_emails WHERE enabled = TRUE ORDER BY email;

-- name: IsEmailAllowed :one
SELECT EXISTS (
  SELECT 1 FROM oauth_allowed_emails WHERE email = ? AND enabled = TRUE
) AS allowed;

-- name: CreateAllowedEmail :execresult
INSERT INTO oauth_allowed_emails (public_id, email, reason, created_by_user_id)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  reason = VALUES(reason),
  created_by_user_id = VALUES(created_by_user_id),
  enabled = TRUE;

-- WithdrawAllowedEmail disables rather than deletes: the exception was a
-- deliberate act and the record of who made it outlives its usefulness.
-- name: WithdrawAllowedEmail :exec
UPDATE oauth_allowed_emails SET enabled = FALSE WHERE id = ?;
