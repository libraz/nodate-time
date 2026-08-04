-- Obligations 1, 3 and 4: the projection guard refuses what the protocol
-- says it refuses, allows what it says it allows, and can be lifted by the
-- documented opt-in.
--
-- Every rejection is checked through nf_conformance_expect_rejected, which
-- insists on SQLSTATE 45000. A write that failed for some other reason —
-- a missing column, a foreign key — is a failure, not a pass.

-- Obligation 1: task_id and task_role are set or cleared together. This
-- would be a CHECK constraint if MySQL allowed one on a column used in a
-- foreign key referential action.
CALL nf_conformance_expect_rejected(
  CONCAT('INSERT INTO calendar_events
            (public_id, workspace_id, calendar_id, title, timezone,
             owner_user_id, created_by_user_id, task_id)
          VALUES (UUID_TO_BIN(UUID(), 0), ', @ws, ', ', @cal, ', ''half linked'', ''UTC'', ',
          @usr, ', ', @usr, ', 4294967295)'),
  'inserting a row with task_id but no task_role must be rejected');

CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET task_role = NULL WHERE id = ', @projected_event),
  'clearing task_role while task_id remains set must be rejected');

-- Obligation 1: a projected event cannot also be recurring. One row cannot
-- stand for both a single task date and an open-ended series.
CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET recurrence_rule = ''{"freq":"WEEKLY"}'' WHERE id = ',
         @projected_event),
  'giving a task-projected event a recurrence rule must be rejected');

-- Obligation 3: the mirrored columns. Each of these has a counterpart in
-- the projecting layer, so writing one alone desyncs the pair.
CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET title = ''renamed'' WHERE id = ', @projected_event),
  'renaming a task-projected event outside the projection engine must be rejected');

CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET start_at = ''2031-01-01 09:00:00'' WHERE id = ',
         @projected_event),
  'moving a task-projected event outside the projection engine must be rejected');

CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET enabled = FALSE WHERE id = ', @projected_event),
  'soft-deleting a task-projected event outside the projection engine must be rejected');

CALL nf_conformance_expect_rejected(
  CONCAT('DELETE FROM calendar_events WHERE id = ', @projected_event),
  'hard-deleting a task-projected event outside the projection engine must be rejected');

-- Obligation 3: creating or clearing the link is itself a guarded write.
CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET task_id = NULL, task_role = NULL WHERE id = ',
         @projected_event),
  'unlinking a task-projected event outside the projection engine must be rejected');

CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET task_id = 4294967294, task_role = ''due'' WHERE id = ',
         @plain_event),
  'linking a plain event to a task outside the projection engine must be rejected');

-- The other half of the boundary. Columns with no counterpart in the
-- projecting layer stay writable, so a calendar UI can still offer them on
-- a projected event. A guard that blocked these would be over-broad, and
-- the product would have to work around its own invariant.
UPDATE calendar_events
SET memo = 'conformance', location = 'somewhere', show_as = 'tentative'
WHERE id = @projected_event;

CALL nf_conformance_assert(
  (SELECT memo = 'conformance' FROM calendar_events WHERE id = @projected_event),
  'columns with no task-side counterpart must stay writable on a projected event');

-- Obligation 4: the opt-in lifts the guard for the connection that sets it.
SET @nf_item_projection_engine = 1;

UPDATE calendar_events SET title = 'renamed by the engine' WHERE id = @projected_event;
CALL nf_conformance_assert(
  (SELECT title = 'renamed by the engine' FROM calendar_events WHERE id = @projected_event),
  'the projection engine opt-in must allow a mirrored column to be written');

SET @nf_item_projection_engine = NULL;

-- Obligation 4: clearing it re-arms the guard. This is what makes it safe
-- to return a connection to a pool — if the variable were sticky, the next
-- request on that connection would inherit an unguarded database.
CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET title = ''renamed again'' WHERE id = ', @projected_event),
  'clearing the opt-in must re-arm the guard on the same connection');

-- Obligation 1 holds even for the engine: the shape rules are not
-- something the opt-in can wave through.
SET @nf_item_projection_engine = 1;
CALL nf_conformance_expect_rejected(
  CONCAT('UPDATE calendar_events SET task_role = NULL WHERE id = ', @projected_event),
  'the shape invariants must hold for the projection engine too');
SET @nf_item_projection_engine = NULL;
