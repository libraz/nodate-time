-- name: ListMemosByCalendar :many
SELECT * FROM calendar_memos
WHERE calendar_id = ? AND enabled = TRUE
ORDER BY sort_weight, created_at;

-- name: GetMemoByPublicID :one
SELECT * FROM calendar_memos
WHERE public_id = ? AND calendar_id = ? AND enabled = TRUE;

-- name: CreateMemo :execresult
INSERT INTO calendar_memos (public_id, workspace_id, calendar_id, created_by_user_id, title, body, sort_weight)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMemo :exec
UPDATE calendar_memos SET title = ?, body = ?, done = ?, sort_weight = ? WHERE id = ?;

-- name: SoftDeleteMemo :exec
UPDATE calendar_memos SET enabled = FALSE WHERE id = ?;
