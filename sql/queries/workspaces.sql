-- Workspaces.
--
-- This application runs one implicit workspace: it has no tenant picker,
-- and every calendar belongs to the same scope. The column and the table
-- are still real, because the contract scopes every shared row by them and
-- a second product on the same database will not be single-tenant.

-- name: GetWorkspaceByID :one
SELECT * FROM workspaces WHERE id = ? AND enabled = TRUE;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspaces WHERE slug = ? AND enabled = TRUE;

-- EnsureWorkspace is the standalone bootstrap: the first request creates
-- the single workspace, and every later one finds it.
-- name: EnsureWorkspace :execresult
INSERT INTO workspaces (public_id, slug, name, timezone, country)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = id;

-- name: AddWorkspaceMember :exec
INSERT INTO workspace_members (public_id, workspace_id, user_id, role, joined_at)
VALUES (?, ?, ?, ?, NOW(3))
ON DUPLICATE KEY UPDATE enabled = TRUE, role = VALUES(role);

-- name: GetWorkspaceMember :one
SELECT * FROM workspace_members
WHERE workspace_id = ? AND user_id = ? AND enabled = TRUE;
