-- Private, time-only request state. Poll answers are intentionally not
-- written to the shared events log; finalizing the poll writes one ordinary
-- calendar event through the existing event creation path.
CREATE TABLE calendar_schedule_polls (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible poll ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  created_by_user_id INT UNSIGNED NULL COMMENT 'Creator; nullable so account removal preserves private history',
  manager_user_id INT UNSIGNED NULL COMMENT 'Current manager; owner/manager may explicitly take over when absent or inactive',
  title VARCHAR(500) NOT NULL,
  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL,
  status ENUM('open','finalized','cancelled') NOT NULL DEFAULT 'open',
  version INT UNSIGNED NOT NULL DEFAULT 1 COMMENT 'Lifecycle and organizer-edit revision',
  response_version INT UNSIGNED NOT NULL DEFAULT 1 COMMENT 'Roster/candidate revision used to reject stale answers',
  selected_slot_public_id BINARY(16) NULL COMMENT 'Chosen candidate UUID after finalization',
  confirmed_event_public_id BINARY(16) NULL COMMENT 'Public ID of the ordinary event created at finalization; retained if that event is deleted',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_schedule_polls_public_id (public_id),
  UNIQUE KEY uniq_calendar_schedule_polls_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_schedule_polls_calendar_status (workspace_id, calendar_id, status, created_at),
  KEY idx_calendar_schedule_polls_manager (workspace_id, manager_user_id, status),
  KEY idx_calendar_schedule_polls_calendar_id_id (calendar_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private schedule poll lifecycle; only finalized events enter shared history';
