-- name: ListEventComments :many
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
WHERE ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL
ORDER BY ec.created_at;

-- name: CreateEventComment :execresult
INSERT INTO calendar_event_comments (public_id, workspace_id, event_id, author_id, body)
VALUES (?, ?, ?, ?, ?);

-- name: GetEventCommentByPublicID :one
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
WHERE ec.public_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL;

-- name: GetEventCommentByPublicIDAndEvent :one
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
WHERE ec.public_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL;

-- name: UpdateEventComment :exec
UPDATE calendar_event_comments SET body = ?, edited_at = NOW(3) WHERE id = ?;

-- name: SoftDeleteEventComment :exec
UPDATE calendar_event_comments SET deleted_at = NOW(3) WHERE id = ?;
