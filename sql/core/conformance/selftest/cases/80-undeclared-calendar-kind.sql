-- A calendar kind no registry declares.
INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'calendar.nf_conformance.undeclared', JSON_OBJECT(), NOW(3)); -- kindscan:undeclared
