-- name: ListMemosByCalendar :many
SELECT * FROM memos WHERE calendar_id = ? ORDER BY sort_order, created_at;

-- name: GetMemoByPublicID :one
SELECT * FROM memos WHERE public_id = ? AND calendar_id = ?;

-- name: CreateMemo :execresult
INSERT INTO memos (public_id, calendar_id, title, body, sort_order, created_by)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateMemo :exec
UPDATE memos SET title = ?, body = ?, done = ?, sort_order = ? WHERE id = ?;

-- name: DeleteMemo :exec
DELETE FROM memos WHERE id = ?;
