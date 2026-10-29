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

-- GetWorkspaceMembership finds the user's membership row whether or not it
-- is enabled. A disabled row is a removal some administrator made, and
-- joining must see it to leave it alone (sql/core/PROTOCOL.md, Identity
-- rule 8).
-- name: GetWorkspaceMembership :one
SELECT id, enabled FROM workspace_members
WHERE workspace_id = ? AND user_id = ?;

-- CountWorkspaceOwners decides whether a joining user becomes the owner: the
-- first to join a workspace nobody owns does.
-- name: CountWorkspaceOwners :one
SELECT COUNT(*) FROM workspace_members
WHERE workspace_id = ? AND role = 'owner' AND enabled = TRUE;

-- CountWorkspaceOwnersForUpdate re-asks under lock, after LockWorkspace, so
-- two first joins cannot both see an ownerless workspace.
-- name: CountWorkspaceOwnersForUpdate :one
SELECT COUNT(*) FROM workspace_members
WHERE workspace_id = ? AND role = 'owner' AND enabled = TRUE
FOR UPDATE;

-- name: LockWorkspace :one
SELECT id FROM workspaces WHERE id = ? FOR UPDATE;

-- InsertWorkspaceMember never revives a row: a removed member comes back
-- only through an explicit addition, which this is not.
-- name: InsertWorkspaceMember :exec
INSERT INTO workspace_members (public_id, workspace_id, user_id, role, invited_at, joined_at)
VALUES (?, ?, ?, ?, NOW(3), NOW(3));

-- GetPersonalCalendar finds the calendar a member is given on joining.
-- name: GetPersonalCalendar :one
SELECT id FROM calendars
WHERE workspace_id = ? AND kind = 'personal' AND owner_user_id = ? AND enabled = TRUE
ORDER BY id
LIMIT 1;

-- name: CreatePersonalCalendar :execresult
INSERT INTO calendars (public_id, workspace_id, kind, name, color, owner_user_id)
VALUES (?, ?, 'personal', ?, ?, ?);

-- GrantPersonalCalendarOwnership is the grant that makes a personal calendar
-- reachable at all. A row that exists keeps its role and colour.
-- name: GrantPersonalCalendarOwnership :exec
INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role, member_color)
VALUES (?, ?, ?, ?, 'owner', ?)
ON DUPLICATE KEY UPDATE enabled = TRUE;

-- name: CreateCalendarSubscription :exec
INSERT INTO calendar_subscriptions (public_id, workspace_id, calendar_id, user_id, display_color)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE enabled = TRUE, display_color = VALUES(display_color);

-- name: GetWorkspaceMember :one
SELECT * FROM workspace_members
WHERE workspace_id = ? AND user_id = ? AND enabled = TRUE;
