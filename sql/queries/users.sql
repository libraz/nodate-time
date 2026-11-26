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

-- Includes disabled accounts: users.email is unique across all rows, so a disabled holder still owns the address.
-- name: GetUserByEmailAnyState :one
SELECT * FROM users WHERE email = ?;

-- name: CreateUser :execresult
INSERT INTO users (public_id, email, display_name, avatar_url, locale, timezone)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, avatar_url = ?, timezone = ?, locale = ?, week_start = ? WHERE id = ?;

-- SetUserAvatarObject points the user at a picture they uploaded. The external
-- URL is deliberately left where it is: the object is preferred while it
-- exists, and the foreign key clears this column if the blob is ever swept, so
-- the provider's picture is what the account falls back to rather than nothing.
-- name: SetUserAvatarObject :exec
UPDATE users SET avatar_storage_object_id = ? WHERE id = ?;

-- name: ClearUserAvatar :exec
UPDATE users SET avatar_storage_object_id = NULL, avatar_url = NULL WHERE id = ?;

-- SeedOAuthAvatarURL adopts a provider's picture for an account that has no
-- avatar of any kind, so a picture the user removed or replaced is never undone.
-- name: SeedOAuthAvatarURL :exec
UPDATE users SET avatar_url = ? WHERE id = ? AND avatar_url IS NULL AND avatar_storage_object_id IS NULL;

-- name: TouchUserLastLogin :exec
UPDATE users SET last_login_at = NOW(3) WHERE id = ?;

-- name: MarkUserEmailVerified :execresult
UPDATE users SET email_verified_at = NOW(3)
WHERE id = ? AND email = ? AND email_verified_at IS NULL;
