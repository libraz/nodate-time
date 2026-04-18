-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByPublicID :one
SELECT * FROM users WHERE public_id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: CreateUser :execresult
INSERT INTO users (public_id, name, email, icon, color, password_hash)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users SET name = ?, icon = ?, color = ? WHERE id = ?;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;
