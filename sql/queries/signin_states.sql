-- name: CreateSigninState :exec
INSERT INTO signin_states (public_id, state_hash, provider, redirect_to, code_verifier, nonce, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- ConsumeSigninState only reads. The caller must follow it with
-- DeleteSigninState and proceed only when that reports one affected row:
-- the delete is what makes the state single-use, so two concurrent
-- callbacks race on it and exactly one wins. Reading alone would let both
-- through.
-- name: ConsumeSigninState :one
SELECT provider, redirect_to, code_verifier, nonce, expires_at
FROM signin_states WHERE state_hash = ?;

-- name: DeleteSigninState :execresult
DELETE FROM signin_states WHERE state_hash = ?;

-- DeleteExpiredSigninStates removes one batch of abandoned round trips and
-- reports how many, so the caller can loop until the backlog is drained.
-- idx_signin_states_expires already leads on expires_at, so the bound here
-- only limits how long one statement holds its rows.
-- name: DeleteExpiredSigninStates :execresult
DELETE FROM signin_states WHERE expires_at < ? ORDER BY expires_at LIMIT ?;
