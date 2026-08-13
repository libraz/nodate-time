-- ====================================
-- calendar_events
-- Calendar events with kind/visibility/show_as classification; nullable
-- start/end for planning-stage placeholders; task_role links to task
-- projection (D1).
-- ====================================
CREATE TABLE calendar_events (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',

  -- Event classification
  kind ENUM('event','block','free','milestone') NOT NULL DEFAULT 'event' COMMENT 'event=regular, block=declarative time frame (work hours, focus), free=available slot, milestone=umbrella/milestone, has no duration semantics',
  visibility ENUM('default','public','private','confidential') NOT NULL DEFAULT 'default' COMMENT 'Who can see event details: default (calendar setting), public (all), private (time only), confidential (owner only)',
  show_as ENUM('busy','free','tentative','oof') NOT NULL DEFAULT 'busy' COMMENT 'Availability display: busy, free, tentative, out-of-office. The iCalendar TRANSP axis — whether the time reads as taken — and nothing more.',
  /**
   * flexibility: whether a confirmed commitment could be moved, which
   * show_as cannot express. A meeting the owner would happily reschedule
   * and one that cannot move are both show_as='busy'; treating either as
   * simply unavailable is what makes coordinating across calendars a
   * conversation rather than a lookup.
   *
   * The two axes stay separate on purpose. Overloading show_as with
   * movability would put non-iCalendar values into the column every
   * external consumer reads as TRANSP, so a free/busy export would start
   * lying to anyone outside this database.
   *
   *   fixed        cannot move (default; the safe reading of a row
   *                written by anything that predates this column)
   *   negotiable   the owner is willing to move it
   *   conditional  movable, but subject to something outside this row —
   *                another party's agreement, a cost, a dependency
   */
  flexibility ENUM('fixed','negotiable','conditional') NOT NULL DEFAULT 'fixed' COMMENT 'Whether the commitment can be moved, independent of whether the time reads as busy. Combined with show_as to derive a displayed availability mark.',

  title VARCHAR(500) NOT NULL COMMENT 'Event title',
  all_day BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'All-day event flag',
  start_at DATETIME(3) NULL COMMENT 'Start time (UTC or with timezone context); NULL = undated (planning-stage placeholder)',
  end_at DATETIME(3) NULL COMMENT 'End time; NULL = undated (planning-stage placeholder)',
  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'UTC' COMMENT 'IANA timezone identifier; resolved from event > user > workspace > UTC',

  location VARCHAR(500) NULL COMMENT 'Location text',
  memo MEDIUMTEXT NULL COMMENT 'Free-form notes (markdown)',
  url VARCHAR(2048) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'Meeting link or related URL',

  -- Ownership: determines whose layer this event belongs to
  owner_user_id INT UNSIGNED NOT NULL COMMENT 'Event owner (whose color/layer). Only owner, managers, or can_edit attendees may edit',
  created_by_user_id INT UNSIGNED NOT NULL COMMENT 'Actual creator (may differ from owner for manager delegation)',

  -- Block metadata
  block_label VARCHAR(100) NULL COMMENT 'Label for block-kind events (e.g., Working, Focus Time, Out of Office)',

  -- Recurrence (RFC 5545 subset stored as JSON)
  recurrence_rule JSON NULL COMMENT 'Recurrence rule: {freq, interval, byDay, byMonthDay, bySetPos, until, count}',
  recurrence_end DATETIME(3) NULL COMMENT 'Computed end date for recurrence expansion queries',
  recurrence_exceptions JSON DEFAULT NULL COMMENT 'Array of ISO 8601 occurrence starts to skip when expanding this rule. Cancelling one occurrence is an entry here, never a row.',
  /**
   * A recurring series is one row plus two ways of departing from it,
   * and they are not interchangeable:
   *
   *   cancel one occurrence  -> add its start to recurrence_exceptions
   *   change one occurrence  -> a second row naming this one as parent
   *
   * Splitting them this way keeps exactly one representation of each
   * outcome. Allowing a row to also mean "cancelled" would give a
   * consumer two places to look before it could say whether an
   * occurrence happens, and the two would eventually disagree.
   *
   * An override row carries the changed occurrence in the ordinary
   * columns and has no recurrence_rule of its own; it is a leaf.
   */
  recurrence_parent_id INT UNSIGNED NULL COMMENT 'Set on an override row: the recurring event whose single occurrence this row replaces. NULL on ordinary and master rows.',
  recurrence_original_start DATETIME(3) NULL COMMENT 'Set on an override row: the start the occurrence would have had under the parent rule. Identifies which occurrence is replaced, so moving the override does not lose track of what it overrides.',

  /**
   * notification_offset: how long before an occurrence its reminder is
   * due. Applies to every occurrence of a recurring event, not only the
   * first — which is why the record of what has already been sent lives
   * in calendar_event_reminders, keyed by occurrence, rather than in a
   * single column here. A column can only remember one answer, and a
   * series needs one per week.
   */
  notification_offset INT NULL COMMENT 'Minutes before an occurrence to send its reminder; NULL = no reminder. Applies to every occurrence of a recurring event; what has already been sent is recorded in calendar_event_reminders.',

  -- Cross-module link to nodate-flow tasks
  task_id INT UNSIGNED NULL COMMENT 'Linked task (optional, for task-calendar sync)',
  task_role ENUM('due','scheduled') NULL COMMENT 'When task_id IS NOT NULL: which task field this event represents. due=task.due_on, scheduled=time-blocked (multi-link allowed).',
  /**
   * task_singleton_role: names the role only when the projection is one
   * a task may hold at most once, and only while the row is live. NULL
   * for everything else, so those rows leave the unique key below
   * entirely — MySQL never treats an index entry containing NULL as a
   * duplicate.
   *
   * The two roles are not the same kind of link and the schema has to
   * say so. `due` mirrors a single task field, so a second live one
   * would mean the task has two due dates. `scheduled` is a time block,
   * and the column comment above has always said a task may hold
   * several — a key that forbade the second one contradicted the
   * documented model and made adding a second block fail.
   *
   * Excluding soft-deleted rows is the other half. A disabled
   * projection keeps its task_id and task_role on purpose: that is the
   * record of what the row projected, and the projection guard forbids
   * clearing it outside the engine. Left in the key, that tombstone
   * collided with the next projection and made the task permanently
   * unschedulable.
   */
  task_singleton_role VARCHAR(16) GENERATED ALWAYS AS (IF(enabled AND task_role = 'due', task_role, NULL)) VIRTUAL COMMENT 'The task_role when it is a role a task may hold at most once and the row is live; NULL otherwise. Exists only to scope uniq_calendar_events_task_singleton_role to live singleton projections.',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  flags JSON NULL COMMENT 'Structured per-event markers (non_working_day, auto_snapped, etc.); unknown keys preserved.',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Soft-delete flag; FALSE excludes the row from LIST/GET. The single soft-delete signal for this table — propagate via INNER/LEFT JOIN ... AND ce.enabled = TRUE in every consumer view, so a soft-deleted row cannot reappear through a join.',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_events_public_id (public_id),
  UNIQUE KEY uniq_calendar_events_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_events_calendar_range (calendar_id, start_at, end_at),
  KEY idx_calendar_events_workspace_owner (workspace_id, owner_user_id, start_at),
  KEY idx_calendar_events_calendar_recurrence (calendar_id, recurrence_end),
  KEY idx_calendar_events_workspace_range (workspace_id, start_at, end_at),
  KEY idx_calendar_events_task_role (task_id, task_role, enabled),
  UNIQUE KEY uniq_calendar_events_task_singleton_role (task_id, task_singleton_role),
  -- One override per occurrence. Without this, two concurrent edits of
  -- the same occurrence both insert, and the expander has to pick.
  UNIQUE KEY uniq_calendar_events_recurrence_override (recurrence_parent_id, recurrence_original_start),
  FULLTEXT KEY ft_calendar_events_title_memo (title, memo),

  CONSTRAINT fk_calendar_events_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE CASCADE,
  -- CASCADE: an override has no meaning once the series it departs from
  -- is gone, and leaving it behind would surface a stray one-off event
  -- on someone's calendar long after they deleted the series.
  CONSTRAINT fk_calendar_events_recurrence_parent FOREIGN KEY (recurrence_parent_id) REFERENCES calendar_events(id) ON DELETE CASCADE,
  -- fk_calendar_events_task (task_id -> tasks.id) is NOT declared here.
  -- task_id is a core column so that every deployment writes rows of the
  -- same shape, but `tasks` belongs to a product layer and a foreign key
  -- cannot point at a table that may not exist. The layer owning `tasks`
  -- adds the constraint from its own constraints/ directory; where there
  -- is no such layer, task_id is always NULL.

  -- CHECK constraints that do not reference task_id. MySQL 8.4+
  -- forbids CHECK constraints referencing columns used in FK
  -- referential actions (task_id has ON DELETE SET NULL), so these
  -- two invariants live in trg_calendar_events_projection_guard_ins /
  -- _upd, which have no such restriction:
  --   (task_id IS NULL) = (task_role IS NULL)
  --   task_id IS NULL OR recurrence_rule IS NULL
  -- The same triggers reserve task_id, task_role, title, start_at,
  -- end_at and enabled on a projected row for the projection engine.
  CONSTRAINT chk_calendar_events_start_end_pair CHECK (start_at IS NULL OR end_at IS NOT NULL),
  CONSTRAINT chk_calendar_events_recurrence_requires_start CHECK (start_at IS NOT NULL OR recurrence_rule IS NULL),
  CONSTRAINT chk_calendar_events_notification_requires_start CHECK (start_at IS NOT NULL OR notification_offset IS NULL),
  CONSTRAINT chk_calendar_events_chronology CHECK (end_at IS NULL OR start_at IS NULL OR end_at >= start_at),
  CONSTRAINT chk_calendar_events_milestone_no_recurrence CHECK (kind <> 'milestone' OR recurrence_rule IS NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar events with kind/visibility/show_as classification; nullable start/end for planning-stage placeholders; task_role links to task projection. Soft-delete is signalled solely by enabled=FALSE (no deleted_at column); consumer views must propagate enabled=TRUE on every JOIN to honour soft-delete.';
