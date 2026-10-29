-- Nullable columns inside unique keys.
--
-- The soft-delete rule next door leans on one property of MySQL: an
-- index entry containing NULL is never a duplicate, which is what lets a
-- tombstone leave the index while live rows stay unique. The property
-- has no idea it is being used on purpose. It applies to every nullable
-- column in every unique key, and where the key was written to bind
-- something it quietly binds nothing for exactly the rows that carry a
-- NULL there.
--
-- That failure is invisible from the schema. `UNIQUE (workspace_id,
-- project_id, name)` reads as "one label of this name per scope" and is
-- one of two completely different rules depending on a nullability three
-- lines above it: for project-scoped labels it holds, and for
-- workspace-wide ones — where project_id IS NULL, and which is most of
-- them — a workspace may hold any number of live labels with the same
-- name. Nothing errors, nothing logs, and the conflict error the API
-- already defines for that case simply never fires.
--
-- So a nullable column in a unique key is a decision, and this file
-- makes it a written one. Two shapes are legitimate:
--
--   Partial uniqueness — the key is meant to be inert for the NULL
--   rows, because those rows have no identity to collide on. A calendar
--   with no provider slug is not a duplicate of another calendar with no
--   provider slug; a webhook test ping stands for no event and is a new
--   dispatch every time. The key constrains the subset that does have an
--   identity, and NULL is how a row says it is not in that subset.
--
--   Disjoint pairs — two keys over two mutually exclusive row kinds,
--   with a CHECK constraint deciding which. Each key binds the kind
--   whose column is set and goes inert for the other. reactions,
--   storage_objects and task_actors are all this shape: every row is
--   covered by exactly one of the two keys, and coverage is complete
--   only because the CHECK forbids the row that would fall through both.
--
-- Anything else is the labels case: a key that claims a constraint and
-- does not have it. The fix is not a sentinel in the base column, which
-- would break the foreign key and lie about the data, but a generated
-- column that names the value instead of carrying NULL — the same
-- device calendar_events.task_singleton_role uses, and what
-- labels.scope_project_id, lenses.scope_project_id and
-- user_view_preferences.scope_key are for.

-- The set that needs justifying: every nullable column sitting in a
-- unique index.
--
-- `active` is excluded because the rule above already pins it. It has to
-- be a nullable generated column derived from enabled and nothing else
-- may be named `active`, so a liveness marker cannot reach this check
-- and no other column can arrive wearing its name.
--
-- Identifiers are copied into utf8mb4_bin so every comparison below is
-- between two columns of this file's own making. information_schema
-- carries its own collation, and joining a temporary table straight
-- against it is an illegal mix of collations on some builds.
DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_cols;
CREATE TEMPORARY TABLE nf_nullable_unique_cols (
  table_name  VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  index_name  VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  column_name VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (table_name, index_name, column_name)
) ENGINE=InnoDB;

INSERT INTO nf_nullable_unique_cols (table_name, index_name, column_name)
SELECT DISTINCT s.table_name, s.index_name, s.column_name
FROM information_schema.statistics s
JOIN information_schema.columns c
  ON  c.table_schema = s.table_schema
  AND c.table_name   = s.table_name
  AND c.column_name  = s.column_name
WHERE s.table_schema = DATABASE()
  AND s.non_unique = 0
  AND s.index_name <> 'PRIMARY'
  AND c.is_nullable = 'YES'
  AND s.column_name <> 'active';

-- Which tables this deployment actually hosts. The allowlist below names
-- tables from both layers, and a deployment that applies core alone has
-- no labels or signals to check — its entries stand down rather than
-- failing the run.
DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_tables;
CREATE TEMPORARY TABLE nf_nullable_unique_tables (
  table_name VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY
) ENGINE=InnoDB;

INSERT INTO nf_nullable_unique_tables (table_name)
SELECT t.table_name
FROM information_schema.tables t
WHERE t.table_schema = DATABASE()
  AND t.table_type = 'BASE TABLE';

-- The allowlist. One row per (table, index, nullable column), and the
-- reason is the entry: it has to say what the NULL rows are and why
-- leaving them unconstrained is the intended answer rather than an
-- oversight. An entry with a blank reason fails, and so does one that no
-- longer describes anything the schema does — see the two checks after
-- the list.
DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_allow;
CREATE TEMPORARY TABLE nf_nullable_unique_allow (
  table_name  VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  index_name  VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  column_name VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  reason      TEXT NOT NULL,
  PRIMARY KEY (table_name, index_name, column_name)
) ENGINE=InnoDB;

INSERT INTO nf_nullable_unique_allow (table_name, index_name, column_name, reason) VALUES
  ('calendar_event_invites', 'uniq_calendar_event_invites_event_attendee', 'event_id',
   'Both halves are set on every invite a writer creates; they go NULL only when the event or the attendee is hard-deleted and the FK sets them so the revoked invite survives. A row that no longer names an event has no grant left to keep singular.'),
  ('calendar_event_invites', 'uniq_calendar_event_invites_event_attendee', 'attendee_id',
   'Same as event_id above: NULL is the post-deletion state, not a state a live invite can be inserted in.'),

  ('calendar_events', 'uniq_calendar_events_recurrence_override', 'recurrence_parent_id',
   'Only an override row replaces one occurrence of a series and needs to be the only one that does. Ordinary and master rows leave this NULL and are not overrides of anything, so there is nothing for the key to make singular.'),
  ('calendar_events', 'uniq_calendar_events_recurrence_override', 'recurrence_original_start',
   'Set with recurrence_parent_id and refused without it by the projection guard, so the pair is either both present on an override or both absent on a row that is not one.'),

  ('calendar_events', 'uniq_calendar_events_task_singleton_role', 'task_id',
   'The key exists to keep one live due projection per task. A row with no task is not a projection and is outside the rule.'),
  ('calendar_events', 'uniq_calendar_events_task_singleton_role', 'task_singleton_role',
   'The generated column deliberately resolves to NULL for everything that may repeat — every scheduled block, and every soft-deleted row of either role — so only live due projections occupy the key.'),

  ('calendar_events', 'uniq_calendar_events_source_uid', 'source_uid_key',
   'One live import per iCalendar UID per calendar. An event created here, a recurrence override, and every soft-deleted row carry no key, and none of them is an import that a second copy of the file could duplicate.'),

  ('calendars', 'uniq_calendars_system_slug', 'system_slug',
   'One calendar per provider feed per workspace. A personal calendar carries no slug and no claim on one, and a workspace holds many of them.'),

  ('events', 'uniq_events_reverses', 'reverses_event_id',
   'Stops two concurrent reversals of one event from both landing and double-cancelling in any projection over the log. An ordinary event reverses nothing and must stay freely appendable.'),

  ('lenses', 'uniq_lenses_public_token_hash', 'public_token_hash',
   'A published lens holds the only hash of its share token, so a token resolves to one lens. An unpublished lens has no token, no share URL, and nothing to be the only holder of.'),

  ('notifications', 'uniq_notifications_recipient_source_channel', 'source_event_id',
   'Collapses a re-fired fan-out of one event to one row per recipient and channel. A notification raised by a background process with no events row has no identity to dedupe on, and two of them are two things to say rather than one said twice.'),

  ('projects', 'uniq_projects_workspace_id_identifier_active', 'identifier',
   'The short project key is unique per workspace once assigned. NULL means unassigned, which is an absence rather than a value, so any number of projects may be waiting for one.'),

  ('reactions', 'uniq_reactions_user_task_emoji', 'task_id',
   'Disjoint pair with uniq_reactions_user_comment_emoji. chk_reactions_target makes exactly one of task_id and comment_id non-NULL, so a comment reaction drops out of this key and is bound by the other one.'),
  ('reactions', 'uniq_reactions_user_comment_emoji', 'comment_id',
   'The other half of the pair: a task reaction leaves comment_id NULL and is bound by uniq_reactions_user_task_emoji instead. Neither kind escapes both keys because the CHECK forbids a row that sets neither column.'),

  ('signals', 'uniq_signals_workspace_source_external_id', 'external_id',
   'Dedupes double delivery from a provider, which identifies its deliveries. A manually dropped or MCP-emitted signal carries no external identifier and each one is a separate submission.'),

  ('storage_objects', 'uniq_storage_objects_workspace_sha', 'workspace_id',
   'Disjoint pair with uniq_storage_objects_user_sha. chk_storage_objects_scope_exclusive makes exactly one of workspace_id and owner_user_id non-NULL, so a user-scoped blob drops out of this key and dedupes under the other one.'),
  ('storage_objects', 'uniq_storage_objects_user_sha', 'owner_user_id',
   'The other half of the pair: a workspace-scoped blob leaves owner_user_id NULL and dedupes under uniq_storage_objects_workspace_sha. Content is deduped within a scope and never across two.'),

  ('task_actors', 'uniq_task_actors_task_id_user_id_role', 'user_id',
   'Disjoint pair with uniq_task_actors_task_id_agent_id_role. chk_task_actors_kind_target makes exactly one of user_id and agent_id non-NULL, so an agent actor drops out of this key and is bound by the other one.'),
  ('task_actors', 'uniq_task_actors_task_id_agent_id_role', 'agent_id',
   'The other half of the pair: a human actor leaves agent_id NULL and is bound by uniq_task_actors_task_id_user_id_role. Every row is covered by exactly one of the two.'),

  ('webhook_deliveries', 'uniq_webhook_deliveries_subscription_event', 'event_public_id',
   'Stops one event being POSTed twice to a subscriber when the fan-out fires twice. A test ping stands for no event and is a fresh dispatch each time it is asked for, so deduplicating pings would make the second press of the button do nothing.');

-- A nullable column in a unique key that nobody has justified. The
-- message names what it found, because the value of this check is in
-- tables that do not exist yet: a new table copying the labels shape
-- lands here rather than in a bug report months later.
SET @nf_nuk_unjustified := (
  SELECT GROUP_CONCAT(
           CONCAT(c.table_name, '.', c.index_name, ' (', c.column_name, ')')
           ORDER BY c.table_name, c.index_name, c.column_name SEPARATOR ', ')
  FROM nf_nullable_unique_cols c
  LEFT JOIN nf_nullable_unique_allow a
    ON  a.table_name  = c.table_name
    AND a.index_name  = c.index_name
    AND a.column_name = c.column_name
  WHERE a.table_name IS NULL);

CALL nf_conformance_assert(
  @nf_nuk_unjustified IS NULL,
  CONCAT('50 unjustified nullable unique key: ', IFNULL(@nf_nuk_unjustified, ''),
         '. A unique index containing a nullable column binds nothing for the rows where that column is NULL; ',
         'either name the value with a generated column or add the index to the allowlist in ',
         'sql/core/conformance/schema/50-nullable-unique-keys.sql with the reason it is meant to be inert'));

-- An allowlist that can outlive what it describes is worse than none: it
-- reads as a review of the current schema and is a record of an old one.
-- Every entry must still name a nullable column of a live unique index,
-- so renaming an index, making the column NOT NULL, or replacing the key
-- with a generated column all strand the entry and fail here. Entries
-- for tables this deployment does not host are skipped — the allowlist
-- spans both schema layers and core alone hosts only some of them.
SET @nf_nuk_stale := (
  SELECT GROUP_CONCAT(
           CONCAT(a.table_name, '.', a.index_name, ' (', a.column_name, ')')
           ORDER BY a.table_name, a.index_name, a.column_name SEPARATOR ', ')
  FROM nf_nullable_unique_allow a
  JOIN nf_nullable_unique_tables t ON t.table_name = a.table_name
  LEFT JOIN nf_nullable_unique_cols c
    ON  c.table_name  = a.table_name
    AND c.index_name  = a.index_name
    AND c.column_name = a.column_name
  WHERE c.table_name IS NULL);

CALL nf_conformance_assert(
  @nf_nuk_stale IS NULL,
  CONCAT('50 stale allowlist entry: ', IFNULL(@nf_nuk_stale, ''),
         '. An allowlist entry must name a nullable column of a unique index that still exists; ',
         'these describe nothing in the schema and must be removed'));

-- A reason is the whole entry, so a blank or one-word one is an
-- allowlist with the justification left out. The floor is deliberately
-- low — it refuses a placeholder, not a short sentence.
SET @nf_nuk_unreasoned := (
  SELECT GROUP_CONCAT(
           CONCAT(a.table_name, '.', a.index_name, ' (', a.column_name, ')')
           ORDER BY a.table_name, a.index_name, a.column_name SEPARATOR ', ')
  FROM nf_nullable_unique_allow a
  WHERE CHAR_LENGTH(TRIM(a.reason)) < 20);

CALL nf_conformance_assert(
  @nf_nuk_unreasoned IS NULL,
  CONCAT('50 allowlist entry without a reason: ', IFNULL(@nf_nuk_unreasoned, ''),
         '. Every allowlist entry must carry a written reason saying which rows the key is inert for ',
         'and why that is correct'));

DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_cols;
DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_tables;
DROP TEMPORARY TABLE IF EXISTS nf_nullable_unique_allow;
SET @nf_nuk_unjustified = NULL;
SET @nf_nuk_stale = NULL;
SET @nf_nuk_unreasoned = NULL;
