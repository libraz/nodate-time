-- ListChecklistItems names the workspace as well as the event: the index is
-- (workspace_id, event_id, sort_weight), and a query that skips its leading
-- column cannot use it at all.
--
-- It has no LIMIT: a checklist is read as a whole -- it is the list somebody
-- ticks off -- and CountChecklistItems is what bounds it at write time.
-- name: ListChecklistItems :many
SELECT * FROM calendar_event_checklist_items
WHERE workspace_id = ? AND event_id = ? AND enabled = TRUE
ORDER BY sort_weight, id;

-- CountChecklistItems is the size of an event's live checklist, read under the
-- event's row lock by the write that would add to it.
-- name: CountChecklistItems :one
SELECT COUNT(*) FROM calendar_event_checklist_items
WHERE workspace_id = ? AND event_id = ? AND enabled = TRUE;

-- name: GetChecklistItemByPublicID :one
SELECT * FROM calendar_event_checklist_items
WHERE public_id = ? AND enabled = TRUE;

-- name: CreateChecklistItem :execresult
INSERT INTO calendar_event_checklist_items (public_id, workspace_id, event_id, created_by_user_id, title, done, sort_weight)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- GetChecklistItemForUpdate reads a live item of one event under its row lock,
-- inside the transaction that writes it, so a partial update merges over the
-- row it replaces.
-- name: GetChecklistItemForUpdate :one
SELECT * FROM calendar_event_checklist_items
WHERE public_id = ? AND event_id = ? AND enabled = TRUE
FOR UPDATE;

-- name: UpdateChecklistItem :exec
UPDATE calendar_event_checklist_items SET title = ?, done = ?, sort_weight = ?
WHERE id = ? AND enabled = TRUE;

-- SoftDeleteChecklistItem retires one live item. The enabled guard makes the
-- affected-row count say whether this call is the one that removed it.
-- name: SoftDeleteChecklistItem :execresult
UPDATE calendar_event_checklist_items SET enabled = FALSE WHERE id = ? AND enabled = TRUE;
