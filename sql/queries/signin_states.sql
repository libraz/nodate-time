-- name: CreateSigninState :exec
INSERT INTO signin_states (public_id, state_hash, provider, redirect_to, code_verifier, nonce, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- ConsumeSigninState deletes and reports whether it deleted anything, so a
-- replayed callback finds nothing. Reading first and deleting after would
-- let two concurrent callbacks both pass.
-- name: ConsumeSigninState :one
SELECT provider, redirect_to, code_verifier, nonce, expires_at
FROM signin_states WHERE state_hash = ?;

-- name: DeleteSigninState :execresult
DELETE FROM signin_states WHERE state_hash = ?;

-- name: DeleteExpiredSigninStates :exec
DELETE FROM signin_states WHERE expires_at < ?;
