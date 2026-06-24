-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_hash, provider, redirect, code_verifier, nonce, expires_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ConsumeOAuthState :one
SELECT provider, redirect, code_verifier, nonce, expires_at FROM oauth_states
WHERE state_hash = ? LIMIT 1;

-- name: DeleteOAuthState :execresult
DELETE FROM oauth_states WHERE state_hash = ?;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at < ?;
