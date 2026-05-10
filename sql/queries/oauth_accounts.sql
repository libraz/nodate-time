-- name: GetOAuthAccount :one
SELECT * FROM oauth_accounts WHERE provider = ? AND provider_subject = ? LIMIT 1;

-- name: ListOAuthAccountsForUser :many
SELECT * FROM oauth_accounts WHERE user_id = ? ORDER BY created_at;

-- name: CreateOAuthAccount :execresult
INSERT INTO oauth_accounts (user_id, provider, provider_subject, email)
VALUES (?, ?, ?, ?);

-- name: DeleteOAuthAccount :exec
DELETE FROM oauth_accounts WHERE user_id = ? AND provider = ?;
