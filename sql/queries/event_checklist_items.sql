-- name: ListChecklistItems :many
SELECT * FROM event_checklist_items
WHERE event_id = ?
ORDER BY sort_order, id;

-- name: GetChecklistItemByPublicID :one
SELECT * FROM event_checklist_items WHERE public_id = ?;

-- name: CreateChecklistItem :execresult
INSERT INTO event_checklist_items (public_id, event_id, title, done, sort_order, created_by)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateChecklistItem :exec
UPDATE event_checklist_items SET title = ?, done = ?, sort_order = ?
WHERE id = ?;

-- name: DeleteChecklistItem :exec
DELETE FROM event_checklist_items WHERE id = ?;
