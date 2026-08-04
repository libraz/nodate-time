-- Fixtures for the schema checks.
-- One workspace, one user, one calendar, and two events: an ordinary one
-- and one that is a projection of a task.
--
-- The slug and email carry a fixed marker so teardown can find them, and
-- so a half-finished run leaves something recognisable rather than rows
-- that look like real data.

INSERT INTO workspaces (public_id, slug, name)
VALUES (UUID_TO_BIN(UUID(), 0), 'nf-conformance', 'nodate core conformance');
SET @ws := LAST_INSERT_ID();

INSERT INTO users (public_id, email, display_name)
VALUES (UUID_TO_BIN(UUID(), 0), 'conformance@nodate.invalid', 'conformance');
SET @usr := LAST_INSERT_ID();

INSERT INTO calendars (public_id, workspace_id, name)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'conformance');
SET @cal := LAST_INSERT_ID();

INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'plain', '2030-01-01 09:00:00',
   '2030-01-01 10:00:00', 'UTC', @usr, @usr);
SET @plain_event := LAST_INSERT_ID();

-- The projected event needs a task_id. Whether that column carries a
-- foreign key depends on which product layers this deployment hosts, and
-- the suite has to behave identically either way, so the fixture points
-- at a synthetic id with foreign key checks off. The guard triggers are
-- unaffected — a BEFORE trigger runs whether or not keys are checked —
-- which is the whole point of the fixture.
SET @fk_checks_were := @@SESSION.foreign_key_checks;
SET SESSION foreign_key_checks = 0;
SET @nf_item_projection_engine = 1;

INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'projected', '2030-01-02 09:00:00',
   '2030-01-02 10:00:00', 'UTC', @usr, @usr, 4294967295, 'due');
SET @projected_event := LAST_INSERT_ID();

SET @nf_item_projection_engine = NULL;
SET SESSION foreign_key_checks = @fk_checks_were;
