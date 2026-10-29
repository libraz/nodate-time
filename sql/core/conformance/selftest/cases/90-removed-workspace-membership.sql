-- A calendar member whose workspace membership was revoked.
INSERT INTO workspace_members (public_id, workspace_id, user_id, role, enabled)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, @usr, 'member', FALSE);
INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, @cal, @usr, 'viewer');
