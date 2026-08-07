-- ListChecklistItems names the workspace as well as the event: the index is
-- (workspace_id, event_id, sort_weight), and a query that skips its leading
-- column cannot use it at all.
--
-- The LIMIT is a cap rather than a page. A checklist belongs to one event and
-- is read as a whole -- it is the list somebody ticks off, so splitting it
-- across pages would be a worse answer than the ceiling. The cap only exists
-- so a runaway writer cannot make one event's modal unbounded.
-- name: ListChecklistItems :many
SELECT * FROM calendar_event_checklist_items
WHERE workspace_id = ? AND event_id = ? AND enabled = TRUE
ORDER BY sort_weight, id
LIMIT ?;

-- name: GetChecklistItemByPublicID :one
SELECT * FROM calendar_event_checklist_items
WHERE public_id = ? AND enabled = TRUE;

-- name: CreateChecklistItem :execresult
INSERT INTO calendar_event_checklist_items (public_id, workspace_id, event_id, created_by_user_id, title, done, sort_weight)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateChecklistItem :exec
UPDATE calendar_event_checklist_items SET title = ?, done = ?, sort_weight = ? WHERE id = ?;

-- name: SoftDeleteChecklistItem :exec
UPDATE calendar_event_checklist_items SET enabled = FALSE WHERE id = ?;
