-- name: ListAllowedEmails :many
SELECT id, email, note, created_by, created_at
FROM oauth_allowed_emails
ORDER BY email;

-- name: IsEmailAllowed :one
SELECT EXISTS (
  SELECT 1 FROM oauth_allowed_emails WHERE email = ?
) AS allowed;

-- name: CreateAllowedEmail :execresult
INSERT INTO oauth_allowed_emails (email, note, created_by)
VALUES (?, ?, ?);

-- name: DeleteAllowedEmail :exec
DELETE FROM oauth_allowed_emails WHERE id = ?;
