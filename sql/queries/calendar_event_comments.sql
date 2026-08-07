-- Both listings name the workspace as well as the event because the index
-- over this table is (workspace_id, event_id, created_at). Leaving the
-- leading column out makes the whole index unusable, so opening an event --
-- the most frequent read there is -- scanned every comment on the deployment.
--
-- They read newest first, which is the opposite of how a thread is displayed
-- and the only order that bounds it usefully: a page taken from the oldest
-- end of a long thread is the part nobody is looking at. The handler turns
-- one page back into reading order, and pages from here go backwards into
-- the history.
--
-- id closes the order, since two comments can land in the same millisecond.
--
-- Every one of these joins the author's avatar object, so a thread carries the
-- key its pictures are signed from instead of costing a lookup per comment.

-- name: ListEventCommentsLatest :many
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       so.storage_key AS user_avatar_storage_key,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE ec.workspace_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL
ORDER BY ec.created_at DESC, ec.id DESC
LIMIT ?;

-- name: ListEventCommentsBefore :many
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       so.storage_key AS user_avatar_storage_key,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE ec.workspace_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL
  AND (
    ec.created_at < sqlc.arg(before_created_at)
    OR (ec.created_at = sqlc.arg(before_created_at) AND ec.id < sqlc.arg(before_id))
  )
ORDER BY ec.created_at DESC, ec.id DESC
LIMIT ?;

-- name: CreateEventComment :execresult
INSERT INTO calendar_event_comments (public_id, workspace_id, event_id, author_id, body)
VALUES (?, ?, ?, ?, ?);

-- name: GetEventCommentByPublicID :one
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       so.storage_key AS user_avatar_storage_key,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE ec.public_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL;

-- name: GetEventCommentByPublicIDAndEvent :one
SELECT ec.*, u.display_name AS user_display_name, u.avatar_url AS user_avatar_url,
       so.storage_key AS user_avatar_storage_key,
       u.public_id AS user_public_id
FROM calendar_event_comments ec
INNER JOIN users u ON u.id = ec.author_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE ec.public_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL;

-- name: UpdateEventComment :exec
UPDATE calendar_event_comments SET body = ?, edited_at = NOW(3) WHERE id = ?;

-- name: SoftDeleteEventComment :exec
UPDATE calendar_event_comments SET deleted_at = NOW(3) WHERE id = ?;
