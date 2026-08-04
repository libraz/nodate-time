-- name: ListChecklistItems :many
SELECT * FROM calendar_event_checklist_items
WHERE event_id = ? AND enabled = TRUE
ORDER BY sort_weight, id;

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
