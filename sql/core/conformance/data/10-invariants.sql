-- Data mode: sweep an existing database for rows no conforming writer
-- could have produced.
--
-- Schema mode proves the database refuses bad writes. This mode is for
-- what the database cannot refuse — obligations that live in the writer,
-- and rows written before a guard existed. It is read-only, so it is safe
-- to point at production.

-- The task link is all-or-nothing. The guard triggers reject this on the
-- way in, so a row here means either a writer on a database without the
-- triggers, or a row that predates them.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0 FROM calendar_events
   WHERE (task_id IS NULL) <> (task_role IS NULL)),
  'found calendar_events rows with exactly one of task_id / task_role set');

-- A projected event may not also be a recurring series.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0 FROM calendar_events
   WHERE task_id IS NOT NULL AND recurrence_rule IS NOT NULL),
  'found task-projected calendar_events rows carrying a recurrence rule');

-- An event row names at most one actor. The three columns cannot be a
-- CHECK constraint, because all three are used in foreign key referential
-- actions, so the rule is only ever as strong as the writers — which is
-- exactly what this mode is for.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0 FROM events
   WHERE (actor_user_id IS NOT NULL)
       + (actor_agent_id IS NOT NULL)
       + (actor_system_source IS NOT NULL) > 1),
  'found events rows naming more than one actor source');

-- Obligation 2, at the coarsest resolution the schema can see: a
-- workspace with calendar data but an empty log means a writer that never
-- appended at all. Per-change coverage is the reconciler's job; this
-- catches the case where a second product was wired up without the log.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 0
   FROM calendars c
   WHERE EXISTS (SELECT 1 FROM calendar_events ce WHERE ce.calendar_id = c.id)
     AND NOT EXISTS (SELECT 1 FROM events e WHERE e.workspace_id = c.workspace_id)),
  'found workspaces holding calendar events but no rows in the event log');

-- A link to a layer this deployment does not host cannot have come from a
-- writer that understood it. Skipped when the task layer is present.
CALL nf_conformance_assert(
  (SELECT COUNT(*) > 0 FROM information_schema.tables
   WHERE table_schema = DATABASE() AND table_name = 'tasks')
  OR (SELECT COUNT(*) = 0 FROM calendar_events WHERE task_id IS NOT NULL),
  'found task-projected calendar_events rows in a deployment with no task layer');
