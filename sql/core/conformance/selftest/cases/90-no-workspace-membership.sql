-- A calendar member who never joined the calendar's workspace.
INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, @cal, @usr, 'viewer');
