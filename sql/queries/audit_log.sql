-- name: InsertAuditLog :exec
INSERT INTO audit_log (calendar_id, entity_type, entity_id, entity_public_id, action, actor_id, summary)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAuditByEntity :many
SELECT al.id, al.action, al.summary, al.created_at,
       u.public_id AS actor_public_id, u.name AS actor_name,
       u.icon AS actor_icon, u.avatar_storage_key AS actor_avatar_key
FROM audit_log al
LEFT JOIN users u ON u.id = al.actor_id
WHERE al.entity_type = ? AND al.entity_public_id = ?
ORDER BY al.created_at ASC, al.id ASC;

-- name: ListAuditByCalendar :many
SELECT al.id, al.entity_type, al.entity_public_id, al.action, al.summary, al.created_at,
       u.public_id AS actor_public_id, u.name AS actor_name,
       u.icon AS actor_icon, u.avatar_storage_key AS actor_avatar_key
FROM audit_log al
LEFT JOIN users u ON u.id = al.actor_id
WHERE al.calendar_id = ?
ORDER BY al.created_at DESC, al.id DESC
LIMIT ?;
