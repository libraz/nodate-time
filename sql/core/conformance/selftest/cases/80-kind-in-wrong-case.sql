-- A declared workspace kind spelt in the wrong case.
INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'Workspace.Member.Added', JSON_OBJECT(), NOW(3)); -- kindscan:undeclared
