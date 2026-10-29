-- Soft-delete and uniqueness.
--
-- A row that can be revoked and created again asks the database two
-- questions, and they are not the same question:
--
--   how many live rows may share this tuple?     (a uniqueness rule)
--   what happened to the rows that are gone?     (a history question)
--
-- Answering both with one `enabled` column inside a UNIQUE key answers
-- neither. `UNIQUE (a, b, enabled)` permits one live row and exactly one
-- tombstone per tuple, so the sequence add / remove / add / remove fails
-- on the second remove: the row being disabled collides with the first
-- tombstone. The failure is permanent, because nothing ever clears a
-- tombstone, and it is invisible to a test that runs a single cycle —
-- which is how the shape survived in a dozen tables at once.
--
-- The contract therefore forbids `enabled` in any unique index and gives
-- two shapes to use instead. Which one a table takes follows from one
-- question: can the same tuple legitimately occur again later as a new
-- thing?
--
--   Recurring tuple — a reaction removed and added again, a label
--   detached and reattached, a project name reused after the project is
--   deleted. Keep the tombstone and scope the key to live rows with a
--   generated liveness column:
--
--       active TINYINT UNSIGNED
--         GENERATED ALWAYS AS (IF(enabled, 1, NULL)) VIRTUAL,
--       UNIQUE KEY uniq_x (a, b, active)
--
--   MySQL never treats an index entry containing NULL as a duplicate, so
--   tombstones leave the index and accumulate freely while live rows
--   stay unique. Where only some values of a column are singletons the
--   generated column names the value instead of carrying 1 — see
--   calendar_events.task_singleton_role below, which constrains `due`
--   to one per task and lets `scheduled` repeat.
--
--   Single grant — a calendar membership, an event invite. The tuple
--   identifies one standing relationship, and a second row would be a
--   second answer to "does this person have access". Key the tuple
--   alone, with no liveness column, and require the writer to revive the
--   existing row rather than insert beside it:
--
--       UNIQUE KEY uniq_x (a, b)
--       INSERT ... ON DUPLICATE KEY UPDATE enabled = TRUE, ...
--
--   A revived grant must rotate anything the revocation was supposed to
--   invalidate. calendar_event_invites carries a token, so reviving it
--   mints a new one; restoring the old token would make revocation
--   reversible by anyone who still held the link.
--
-- The NULL property this file uses on purpose applies to every other
-- nullable column in a unique key too, whether or not anyone meant it
-- to. That generalisation is its own rule, in
-- sql/core/conformance/schema/50-nullable-unique-keys.sql, which is
-- where the second shape above — the value-naming generated column —
-- gets its remaining instances.
--
-- The first check below is the mechanical half. It does not know which
-- shape a table should take, but it refuses the one shape that is always
-- wrong — including in tables that do not exist yet.

CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0
   FROM information_schema.statistics
   WHERE table_schema = DATABASE()
     AND non_unique = 0
     AND column_name = 'enabled'),
  'no unique index may include the enabled column');

-- The liveness marker has to be the agreed expression, or the index it
-- backs means something different from table to table. A column named
-- `active` that is not NULL-when-disabled would silently restore the
-- collision this rule exists to remove.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND column_name = 'active'
     AND (generation_expression = ''
          OR is_nullable <> 'YES'
          OR generation_expression NOT LIKE '%enabled%')),
  'a column named active must be a nullable generated column derived from enabled');

-- calendar_events.task_singleton_role is the core table's instance of
-- the recurring-tuple shape, and the one an implementation is most
-- likely to get wrong, because the whole point is that `scheduled` may
-- repeat while `due` may not.
CALL nf_conformance_assert(
  (SELECT is_nullable = 'YES' AND generation_expression LIKE '%enabled%'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_events'
     AND column_name = 'task_singleton_role'),
  'calendar_events.task_singleton_role must be nullable and generated from enabled');

-- Behavioural half. The projection rows below need the engine flag and,
-- like the fixtures, a synthetic task id: whether task_id carries a
-- foreign key depends on which product layers the deployment hosts, and
-- the suite has to behave the same either way.
SET @fk_checks_were := @@SESSION.foreign_key_checks;
SET SESSION foreign_key_checks = 0;
SET @nf_item_projection_engine = 1;
SET @nf_sdu_task := 4294967293;

-- Two live time blocks on one task. This is what the task_role column
-- comment has always documented and what the old key forbade: adding a
-- second block to a task failed with a duplicate key.
INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'sdu block one', '2030-03-01 09:00:00',
   '2030-03-01 10:00:00', 'UTC', @usr, @usr, @nf_sdu_task, 'scheduled');

INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'sdu block two', '2030-03-02 09:00:00',
   '2030-03-02 10:00:00', 'UTC', @usr, @usr, @nf_sdu_task, 'scheduled');

CALL nf_conformance_assert(
  (SELECT COUNT(*) = 2 FROM calendar_events
   WHERE task_id = @nf_sdu_task AND task_role = 'scheduled' AND enabled = TRUE),
  'a task must be able to hold more than one live scheduled projection');

-- A due projection is a singleton, so a second live one is refused.
INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'sdu due one', '2030-03-03 09:00:00',
   '2030-03-03 10:00:00', 'UTC', @usr, @usr, @nf_sdu_task, 'due');
SET @nf_sdu_due_one := LAST_INSERT_ID();

CALL nf_conformance_expect_duplicate(
  CONCAT('INSERT INTO calendar_events ',
         '(public_id, workspace_id, calendar_id, title, start_at, end_at, timezone, ',
         'owner_user_id, created_by_user_id, task_id, task_role) VALUES (',
         'UUID_TO_BIN(UUID(), 0), ', @ws, ', ', @cal, ", 'sdu due dup', ",
         "'2030-03-04 09:00:00', '2030-03-04 10:00:00', 'UTC', ",
         @usr, ', ', @usr, ', ', @nf_sdu_task, ", 'due')"),
  'a task must not hold two live due projections');

-- Soft-deleting the due projection frees the slot. Unschedule followed
-- by reschedule is the sequence that used to fail for good, because the
-- disabled row kept its place in the key.
UPDATE calendar_events SET enabled = FALSE WHERE id = @nf_sdu_due_one;

INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'sdu due again', '2030-03-05 09:00:00',
   '2030-03-05 10:00:00', 'UTC', @usr, @usr, @nf_sdu_task, 'due');
SET @nf_sdu_due_two := LAST_INSERT_ID();

CALL nf_conformance_assert(
  (SELECT COUNT(*) = 1 FROM calendar_events
   WHERE task_id = @nf_sdu_task AND task_role = 'due' AND enabled = TRUE),
  'a due projection must be re-creatable once the previous one is soft-deleted');

-- The second cycle is the one that mattered. Disabling the replacement
-- produces a second tombstone for the same tuple, which is precisely
-- what the old key could not represent.
UPDATE calendar_events SET enabled = FALSE WHERE id = @nf_sdu_due_two;

CALL nf_conformance_assert(
  (SELECT COUNT(*) = 2 FROM calendar_events
   WHERE task_id = @nf_sdu_task AND task_role = 'due' AND enabled = FALSE),
  'a tuple must tolerate more than one tombstone, so a second delete does not collide with the first');

SET @nf_item_projection_engine = NULL;
SET SESSION foreign_key_checks = @fk_checks_were;
