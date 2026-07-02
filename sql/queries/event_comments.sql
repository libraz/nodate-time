-- name: ListEventComments :many
SELECT ec.*, u.name AS user_name, u.icon AS user_icon, u.public_id AS user_public_id
FROM event_comments ec
INNER JOIN users u ON u.id = ec.user_id
WHERE ec.event_id = ?
ORDER BY ec.created_at;

-- name: CreateEventComment :execresult
INSERT INTO event_comments (public_id, event_id, user_id, body)
VALUES (?, ?, ?, ?);

-- name: GetEventCommentByPublicID :one
SELECT ec.*, u.name AS user_name, u.icon AS user_icon, u.public_id AS user_public_id
FROM event_comments ec
INNER JOIN users u ON u.id = ec.user_id
WHERE ec.public_id = ?;

-- name: GetEventCommentByPublicIDAndEvent :one
SELECT ec.*, u.name AS user_name, u.icon AS user_icon, u.public_id AS user_public_id
FROM event_comments ec
INNER JOIN users u ON u.id = ec.user_id
WHERE ec.public_id = ? AND ec.event_id = ?;

-- name: UpdateEventComment :exec
UPDATE event_comments SET body = ? WHERE id = ?;

-- name: DeleteEventComment :exec
DELETE FROM event_comments WHERE id = ?;
