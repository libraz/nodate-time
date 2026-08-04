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
  recurrence_exceptions JSON DEFAULT NULL COMMENT 'Array of ISO 8601 dates/times to exclude from recurrence',

  notification_offset INT NULL COMMENT 'Minutes before event to send notification; NULL = no notification',
  notified_at DATETIME(3) NULL DEFAULT NULL COMMENT 'Timestamp when notification was sent; NULL = not yet notified',

  -- Cross-module link to nodate-flow tasks
  task_id INT UNSIGNED NULL COMMENT 'Linked task (optional, for task-calendar sync)',
  task_role ENUM('due','scheduled') NULL COMMENT 'When task_id IS NOT NULL: which task field this event represents. due=task.due_on, scheduled=time-blocked (multi-link allowed).',
  /**
   * task_role_key: de-NULLed projection of task_role used to build a UNIQUE
   * key over (task_id, task_role). MySQL UNIQUE indexes treat NULLs as
   * distinct, which would let two NULL task_role rows coexist for the same
   * task_id and silently weaken the (task_id, task_role) invariant. By
   * coalescing NULL to the empty string in a STORED generated column we get
   * a NOT NULL surrogate that participates in the UNIQUE without losing the
   * "absent role" sentinel.
   */
  task_role_key VARCHAR(32) GENERATED ALWAYS AS (COALESCE(task_role, '')) STORED NOT NULL COMMENT 'De-NULLed surrogate for task_role; empty string when task_role IS NULL. Exists solely to power uniq_calendar_events_task_role_key over (task_id, task_role_key).',

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
  UNIQUE KEY uniq_calendar_events_task_role_key (task_id, task_role_key),
  FULLTEXT KEY ft_calendar_events_title_memo (title, memo),

  CONSTRAINT fk_calendar_events_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_events_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE CASCADE,
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
