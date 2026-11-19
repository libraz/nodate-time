CREATE TABLE calendar_schedule_poll_slots (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible candidate ID',
  poll_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_schedule_polls.id',
  start_at DATETIME(3) NOT NULL COMMENT 'Candidate start instant stored as UTC',
  end_at DATETIME(3) NOT NULL COMMENT 'Candidate end instant stored as UTC',
  availability ENUM('open','soft','hard') NOT NULL COMMENT 'Latest aggregate availability; no event details are stored here',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Hard conflicts are kept for the manager to see but removed from response requirements',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_schedule_poll_slots_public_id (public_id),
  UNIQUE KEY uniq_calendar_schedule_poll_slots_poll_public_id (poll_id, public_id),
  UNIQUE KEY uniq_calendar_schedule_poll_slots_poll_id_id (poll_id, id),
  KEY idx_calendar_schedule_poll_slots_poll_enabled (poll_id, enabled, start_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private schedule poll candidate instants';
