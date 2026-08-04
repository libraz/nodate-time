-- Instance administrators. The contract keeps this as a table rather than
-- a flag on users: revoking admin should leave a record that it was ever
-- granted, and by whom.

-- name: IsInstanceAdmin :one
SELECT EXISTS (
  SELECT 1 FROM instance_admins
  WHERE user_id = ? AND revoked_at IS NULL AND enabled = TRUE
) AS is_admin;

-- name: GrantInstanceAdmin :exec
INSERT INTO instance_admins (public_id, user_id, granted_by_user_id)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE revoked_at = NULL, enabled = TRUE, granted_by_user_id = VALUES(granted_by_user_id);

-- name: RevokeInstanceAdmin :exec
UPDATE instance_admins SET revoked_at = NOW(3) WHERE user_id = ? AND revoked_at IS NULL;

-- name: ListInstanceAdmins :many
SELECT ia.*, u.public_id AS user_public_id, u.email, u.display_name
FROM instance_admins ia
INNER JOIN users u ON u.id = ia.user_id
WHERE ia.revoked_at IS NULL AND ia.enabled = TRUE
ORDER BY ia.granted_at;
