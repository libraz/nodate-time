-- Memos are paged in the order the list is kept in, not by recency: the
-- weight is what the owner arranged, so a page boundary has to fall inside
-- that arrangement rather than across a second one.
--
-- id closes the order. Two memos can share a weight and a creation instant,
-- and a keyset cursor over an order that is not total either repeats a row
-- at the seam or steps over one.

-- name: ListMemosByCalendarFirstPage :many
SELECT * FROM calendar_memos
WHERE calendar_id = ? AND enabled = TRUE
ORDER BY sort_weight, created_at, id
LIMIT ?;

-- name: ListMemosByCalendarAfter :many
SELECT * FROM calendar_memos
WHERE calendar_id = ? AND enabled = TRUE
  AND (
    sort_weight > sqlc.arg(after_sort_weight)
    OR (sort_weight = sqlc.arg(after_sort_weight)
        AND created_at > sqlc.arg(after_created_at))
    OR (sort_weight = sqlc.arg(after_sort_weight)
        AND created_at = sqlc.arg(after_created_at)
        AND id > sqlc.arg(after_id))
  )
ORDER BY sort_weight, created_at, id
LIMIT ?;

-- name: GetMemoByPublicID :one
SELECT * FROM calendar_memos
WHERE public_id = ? AND calendar_id = ? AND enabled = TRUE;

-- GetMemoForUpdate is the read a write merges over: it takes the row lock, so
-- the fields it returns are the ones the write replaces, and a memo deleted
-- in the meantime is simply not there.
-- name: GetMemoForUpdate :one
SELECT * FROM calendar_memos
WHERE public_id = ? AND calendar_id = ? AND enabled = TRUE FOR UPDATE;

-- GetMemoCursor resolves the memo a page cursor names. It matches deleted
-- memos too: one deleted between two pages still has an ordering position,
-- and refusing its cursor would break a list that is being walked.
-- name: GetMemoCursor :one
SELECT * FROM calendar_memos WHERE public_id = ? AND calendar_id = ?;

-- name: CreateMemo :execresult
INSERT INTO calendar_memos (public_id, workspace_id, calendar_id, created_by_user_id, title, body, sort_weight)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMemo :exec
UPDATE calendar_memos SET title = ?, body = ?, done = ?, sort_weight = ? WHERE id = ? AND enabled = TRUE;

-- name: SoftDeleteMemo :execresult
UPDATE calendar_memos SET enabled = FALSE WHERE id = ? AND enabled = TRUE;
