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

-- CountInstanceAdminsForUpdate guards the last-administrator case. Nothing in
-- the API grants these rights, so an instance that loses its last one can only
-- get another by a statement typed against this database. The count has to be
-- held across the revocation or two concurrent ones both pass: each sees the
-- other still counted, and between them they remove everybody.
--
-- The cost is that this locks every live grant rather than a narrowed set --
-- the question is how many there are in total, so there is nothing to narrow
-- by -- which serialises every revocation on the deployment against every
-- other. That is the intended trade and not an oversight: revoking an
-- administrator is rare and deliberate, and the alternative to queueing them
-- is the race above.
-- name: CountInstanceAdminsForUpdate :one
SELECT COUNT(*) FROM instance_admins
WHERE revoked_at IS NULL AND enabled = TRUE FOR UPDATE;

-- name: ListInstanceAdmins :many
SELECT ia.*, u.public_id AS user_public_id, u.email, u.display_name
FROM instance_admins ia
INNER JOIN users u ON u.id = ia.user_id
WHERE ia.revoked_at IS NULL AND ia.enabled = TRUE
ORDER BY ia.granted_at;
