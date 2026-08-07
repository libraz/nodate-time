-- ListEventComments names the workspace as well as the event because the
-- index over this table is (workspace_id, event_id, created_at). Leaving the
-- leading column out makes the whole index unusable, so opening an event --
-- the most frequent read there is -- scanned every comment on the deployment.
-- name: ListEventComments :many
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
WHERE ec.workspace_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL
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
