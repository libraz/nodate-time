-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_hash, provider, redirect, expires_at)
VALUES (?, ?, ?, ?);

-- name: ConsumeOAuthState :one
SELECT provider, redirect, expires_at FROM oauth_states
WHERE state_hash = ? LIMIT 1;

-- name: DeleteOAuthState :exec
DELETE FROM oauth_states WHERE state_hash = ?;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at < ?;
