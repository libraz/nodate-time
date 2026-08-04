-- Users and their credentials.
--
-- The password lives in identities, not on the user: a user may hold a
-- local identity plus any number of provider identities, and the contract
-- keeps them in one table so signing in through a second provider does not
-- silently create a second account.

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ? AND enabled = TRUE;

-- name: GetUserByIDForUpdate :one
SELECT * FROM users WHERE id = ? FOR UPDATE;

-- name: GetUserByPublicID :one
SELECT * FROM users WHERE public_id = ? AND enabled = TRUE;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ? AND enabled = TRUE;

-- name: CreateUser :execresult
INSERT INTO users (public_id, email, display_name, avatar_url, locale, timezone)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, avatar_url = ?, timezone = ?, locale = ? WHERE id = ?;

-- name: SetUserAvatarObject :exec
UPDATE users SET avatar_storage_object_id = ?, avatar_url = NULL WHERE id = ?;

-- name: ClearUserAvatar :exec
UPDATE users SET avatar_storage_object_id = NULL, avatar_url = NULL WHERE id = ?;

-- name: TouchUserLastLogin :exec
UPDATE users SET last_login_at = NOW(3) WHERE id = ?;
