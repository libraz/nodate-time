-- name: InsertAuditLog :exec
INSERT INTO audit_log (calendar_id, entity_type, entity_id, entity_public_id, action, actor_id, summary)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAuditByEntity :many
-- Most-recent-first: a fixed LIMIT with no cursor must show the latest
-- changes, not truncate to whatever happened first in the entity's history.
SELECT al.id, al.action, al.summary, al.created_at,
       u.public_id AS actor_public_id, u.name AS actor_name,
       u.icon AS actor_icon, u.avatar_storage_key AS actor_avatar_key
FROM audit_log al
LEFT JOIN users u ON u.id = al.actor_id
WHERE al.entity_type = ? AND al.entity_public_id = ?
  AND al.calendar_id = ?
ORDER BY al.id DESC
LIMIT ?;

-- name: ListAuditByCalendar :many
-- Ordered and keyset-paginated by id alone (not created_at): id is a strictly
-- monotonic insertion-order key, so this is already equivalent to ordering by
-- time, and keeps the ORDER BY and the "id < after_id" cursor condition from
-- ever disagreeing about tie-breaking.
SELECT al.id, al.entity_type, al.entity_public_id, al.action, al.summary, al.created_at,
       u.public_id AS actor_public_id, u.name AS actor_name,
       u.icon AS actor_icon, u.avatar_storage_key AS actor_avatar_key
FROM audit_log al
LEFT JOIN users u ON u.id = al.actor_id
WHERE al.calendar_id = ?
  AND (sqlc.arg(after_id) = 0 OR al.id < sqlc.arg(after_id))
ORDER BY al.id DESC
LIMIT ?;
