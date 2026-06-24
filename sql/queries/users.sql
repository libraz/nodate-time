-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByPublicID :one
SELECT * FROM users WHERE public_id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: CreateUser :execresult
INSERT INTO users (public_id, name, email, icon, color, password_hash)
VALUES (?, ?, ?, ?, ?, ?);

-- name: CreateUserWithRole :execresult
INSERT INTO users (public_id, name, email, icon, color, password_hash, is_admin)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = ? WHERE id = ?;

-- name: UpdateUser :exec
UPDATE users SET name = ?, icon = ?, color = ? WHERE id = ?;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: UpdateUserAvatar :exec
UPDATE users SET avatar_storage_key = ?, avatar_content_type = ? WHERE id = ?;

-- name: ClearUserAvatar :exec
UPDATE users SET avatar_storage_key = NULL, avatar_content_type = NULL WHERE id = ?;
