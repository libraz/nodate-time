-- ====================================
-- calendar_event_reminders
-- One row per reminder that has been sent, keyed by the occurrence it
-- was sent for.
--
-- Reminders used to be claimed on calendar_events.notified_at: one
-- column, one claim, one reminder for the lifetime of the row. That is
-- the right shape for an event that happens once and the wrong shape for
-- a series — "every Monday, 15 minutes before" rang on the first Monday
-- and was silent for the remaining fifty-one, because the column was
-- already set. Adding a recurrence rule to an existing event that had
-- already fired meant it never rang again at all.
--
-- The claim is an INSERT rather than an UPDATE because insertion is
-- naturally once-only: the unique key below decides the winner, and the
-- loser gets a duplicate-key error instead of a zero-rows-affected count
-- it has to remember to check. Releasing a claim after a failed dispatch
-- is a DELETE, which is why there is no soft-delete flag here — a
-- released claim must genuinely stop existing so the next tick can take
-- it, and a tombstone that still occupied the unique key would make the
-- retry impossible.
--
-- Deliberately without public_id, sort_weight or notes: nothing outside
-- the scheduler ever addresses one of these rows, and the columns the
-- table template carries are for entities the API exposes.
-- ====================================
CREATE TABLE calendar_event_reminders (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id; carried so a workspace teardown reaches these rows directly',
  event_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_events.id',

  /**
   * occurrence_start: the start instant of the specific occurrence this
   * reminder was for, in UTC.
   *
   * For a non-recurring event it equals calendar_events.start_at. For a
   * series it is one expansion of the rule, which is what makes the
   * claim per-occurrence rather than per-row. Stored to millisecond
   * precision so it matches the column it is derived from exactly; a
   * truncated copy would fail to match and re-send.
   */
  occurrence_start DATETIME(3) NOT NULL COMMENT 'UTC start of the occurrence this reminder covered; equals calendar_events.start_at for non-recurring events',

  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'When the claim was taken, which is also when the reminder was sent',

  UNIQUE KEY uniq_calendar_event_reminders_occurrence (event_id, occurrence_start),
  KEY idx_calendar_event_reminders_workspace (workspace_id),

  CONSTRAINT fk_calendar_event_reminders_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_reminders_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Sent-reminder claims, one per (event, occurrence). Replaces the single calendar_events.notified_at claim, which could only ever fire once per row.';
