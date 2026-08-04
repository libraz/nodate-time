-- Remove everything the fixtures created.
--
-- Rows are found by their marker rather than by the variables the setup
-- assigned, so teardown also cleans up after a run that failed partway and
-- left the session behind.

SET @nf_conformance_ws := (SELECT id FROM workspaces WHERE slug = 'nf-conformance');

SET @fk_checks_were := @@SESSION.foreign_key_checks;
SET SESSION foreign_key_checks = 0;
SET @nf_item_projection_engine = 1;

DELETE FROM calendar_events WHERE workspace_id = @nf_conformance_ws;
DELETE FROM events WHERE workspace_id = @nf_conformance_ws;
DELETE FROM calendars WHERE workspace_id = @nf_conformance_ws;
DELETE FROM workspaces WHERE id = @nf_conformance_ws;
DELETE FROM users WHERE email = 'conformance@nodate.invalid';

SET @nf_item_projection_engine = NULL;
SET SESSION foreign_key_checks = @fk_checks_were;

DROP PROCEDURE IF EXISTS nf_conformance_assert;
DROP PROCEDURE IF EXISTS nf_conformance_expect_rejected;
