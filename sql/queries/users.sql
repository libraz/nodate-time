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

-- SetUserAvatarObject points the user at a picture they uploaded. The external
-- URL is deliberately left where it is: the object is preferred while it
-- exists, and the foreign key clears this column if the blob is ever swept, so
-- the provider's picture is what the account falls back to rather than nothing.
-- name: SetUserAvatarObject :exec
UPDATE users SET avatar_storage_object_id = ? WHERE id = ?;

-- name: ClearUserAvatar :exec
UPDATE users SET avatar_storage_object_id = NULL, avatar_url = NULL WHERE id = ?;

-- name: TouchUserLastLogin :exec
UPDATE users SET last_login_at = NOW(3) WHERE id = ?;

-- name: MarkUserEmailVerified :execresult
UPDATE users SET email_verified_at = NOW(3)
WHERE id = ? AND email = ? AND email_verified_at IS NULL;
