-- Obligation 2: the event log behaves the way a cross-process consumer
-- needs it to.
--
-- Whether a given writer actually appends is a property of that writer,
-- not of the schema, and is checked by the data mode. What is checked here
-- is that the log can carry the guarantees the protocol promises.

INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at, actor_user_id)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'conformance.first', JSON_OBJECT('n', 1), NOW(3), @usr);
SET @first_event := LAST_INSERT_ID();

INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at, actor_user_id)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'conformance.second', JSON_OBJECT('n', 2), NOW(3), NULL);
SET @second_event := LAST_INSERT_ID();

-- Monotonic ids are the whole basis of `WHERE id > last_seen`. Without
-- them a tailer either misses rows or replays them.
CALL nf_conformance_assert(
  @second_event > @first_event,
  'events.id must increase with each append so a consumer can tail by id');

-- A system-originated event has no acting user. An implementation that
-- required one would have to invent a synthetic user for its own
-- background work.
CALL nf_conformance_assert(
  (SELECT actor_user_id IS NULL FROM events WHERE id = @second_event),
  'events must accept an append with no acting user');

-- Payloads round-trip as JSON rather than as text, so a consumer can query
-- into them and an unknown key survives a read.
CALL nf_conformance_assert(
  (SELECT JSON_EXTRACT(payload_json, '$.n') = 2 FROM events WHERE id = @second_event),
  'events.payload_json must be queryable JSON');

-- occurred_at carries millisecond precision. Truncating to seconds would
-- make ordering within a burst depend entirely on id, which is assigned by
-- the database rather than by the writer.
CALL nf_conformance_assert(
  (SELECT datetime_precision = 3 FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'events' AND column_name = 'occurred_at'),
  'events.occurred_at must keep millisecond precision');

-- The log is workspace-scoped and disappears with its workspace, so a
-- deleted tenant leaves nothing behind for a tailer to serve.
CALL nf_conformance_assert(
  (SELECT COUNT(*) FROM information_schema.referential_constraints
   WHERE constraint_schema = DATABASE()
     AND table_name = 'events'
     AND referenced_table_name = 'workspaces'
     AND delete_rule = 'CASCADE') = 1,
  'events must cascade away with its workspace');

-- The log carries an entity pointer per domain so a feed scoped to one
-- calendar can read it directly. An implementation lacking the column
-- would keep a second history table beside the log, and the two would
-- disagree the first time a writer updated one and not the other.
CALL nf_conformance_assert(
  (SELECT is_nullable = 'YES' AND column_type = 'int unsigned'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'events' AND column_name = 'calendar_id'),
  'events.calendar_id must exist and be nullable so a per-calendar feed reads the log itself');
