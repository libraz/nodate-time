-- Structural checks: the shapes an implementation is allowed to rely on.
--
-- These are deliberately narrow. They assert the properties another
-- product's code would break on, not the full column list — a suite that
-- pinned every column would fail on additive changes the contract
-- explicitly permits.

-- Every core table is present.
CALL nf_conformance_assert(
  (SELECT COUNT(*) FROM information_schema.tables
   WHERE table_schema = DATABASE()
     AND table_name IN ('workspaces', 'users', 'calendars', 'calendar_events', 'events')) = 5,
  'the core tables workspaces, users, calendars, calendar_events and events must all exist');

-- The event log's primary key is what makes it tailable. A consumer polls
-- `WHERE id > last_seen`, so the column has to be monotonic and wide
-- enough that a long-lived deployment never wraps.
CALL nf_conformance_assert(
  (SELECT column_type = 'bigint unsigned' AND extra LIKE '%auto_increment%'
   FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'events' AND column_name = 'id'),
  'events.id must be BIGINT UNSIGNED AUTO_INCREMENT so consumers can tail the log by id');

-- Public identity is a 16-byte UUID everywhere, never the auto-increment.
-- An implementation that exposed the internal id would leak row counts and
-- break the moment two deployments merge.
CALL nf_conformance_assert(
  (SELECT COUNT(*) FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name IN ('workspaces', 'users', 'calendars', 'calendar_events', 'events')
     AND column_name = 'public_id'
     AND column_type = 'binary(16)'
     AND is_nullable = 'NO') = 5,
  'public_id must be a NOT NULL BINARY(16) on every core table');

-- Cross-layer columns must stay optional, so a deployment that does not
-- host the layer giving them meaning can still insert.
CALL nf_conformance_assert(
  (SELECT is_nullable = 'YES'
   FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'calendar_events' AND column_name = 'task_id'),
  'calendar_events.task_id must be nullable so a deployment without a task layer can write events');

CALL nf_conformance_assert(
  (SELECT COUNT(*) FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'events'
     AND column_name IN ('task_id', 'actor_user_id')
     AND is_nullable = 'YES') = 2,
  'events.task_id and events.actor_user_id must be nullable');

-- Availability is two independent columns. An implementation that dropped
-- flexibility would have nowhere to record that a commitment can move, and
-- the pressure would be to overload show_as instead — which is the one
-- thing the contract forbids, because show_as is what every external
-- free/busy consumer reads.
CALL nf_conformance_assert(
  (SELECT column_type = "enum('fixed','negotiable','conditional')"
      AND is_nullable = 'NO'
      AND column_default = 'fixed'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_events' AND column_name = 'flexibility'),
  'calendar_events.flexibility must be a NOT NULL enum defaulting to fixed');

-- The default is load-bearing: a writer that predates the column, or one
-- that simply omits it, must produce a row that reads as immovable rather
-- than one that advertises availability its owner never offered.
CALL nf_conformance_assert(
  (SELECT flexibility = 'fixed' FROM calendar_events WHERE id = @plain_event),
  'an event inserted without naming flexibility must default to fixed');

-- Access and display preference are separate tables. An implementation
-- that dropped calendar_members would have to gate on the subscription
-- instead, which grants nothing — anyone who ever set a sidebar colour
-- would hold access nobody granted them.
CALL nf_conformance_assert(
  (SELECT column_type = "enum('owner','manager','editor','viewer')" AND is_nullable = 'NO'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_members' AND column_name = 'role'),
  'calendar_members.role must be a NOT NULL enum of owner, manager, editor and viewer');

CALL nf_conformance_assert(
  (SELECT column_default = 'viewer'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_members' AND column_name = 'role'),
  'calendar_members.role must default to the least privilege, so a writer that omits it cannot grant access');

-- One grant per (calendar, user), revoked rows included. Without this a
-- re-add leaves the older grant behind for an access check to find.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 1 FROM information_schema.statistics
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_members'
     AND index_name = 'uniq_calendar_members_calendar_user'
     AND non_unique = 0
     AND column_name = 'calendar_id'),
  'calendar_members must be unique per (calendar_id, user_id)');

-- Sharing is membership, not a calendar kind. An implementation that
-- added 'shared' to the enum would have two encodings of one fact, and
-- the redundant one does not carry the behaviour: a calendar labelled
-- shared but still naming an owner is deleted along with that user.
CALL nf_conformance_assert(
  (SELECT column_type = "enum('personal','system')"
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendars' AND column_name = 'kind'),
  'calendars.kind must remain personal and system only; sharing is calendar_members');

-- A calendar a group shares has to be able to belong to no one, or the
-- owner FK cascade takes everyone else's history when one member goes.
CALL nf_conformance_assert(
  (SELECT is_nullable = 'YES'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendars' AND column_name = 'owner_user_id'),
  'calendars.owner_user_id must be nullable so a shared calendar can outlive any one member');

-- Events, by contrast, always belong to someone: the owner is what
-- decides whose layer and colour they appear on, so there is no
-- unowned event for an importer to invent.
CALL nf_conformance_assert(
  (SELECT is_nullable = 'NO'
   FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'calendar_events' AND column_name = 'owner_user_id'),
  'calendar_events.owner_user_id must be NOT NULL so every event lands on a layer');

-- The guard triggers are part of the contract, not an optional extra: an
-- implementation that loaded the tables but skipped them would accept
-- writes this document says are refused.
CALL nf_conformance_assert(
  (SELECT COUNT(*) FROM information_schema.triggers
   WHERE trigger_schema = DATABASE()
     AND trigger_name IN ('trg_calendar_events_projection_guard_ins',
                          'trg_calendar_events_projection_guard_upd',
                          'trg_calendar_events_projection_guard_del')) = 3,
  'all three calendar_events projection guard triggers must be installed');
