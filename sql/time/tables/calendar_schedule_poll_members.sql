CREATE TABLE calendar_schedule_poll_members (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  poll_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_schedule_polls.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id; public responses use the user public ID',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'FALSE when removed from this poll; re-adding requires fresh answers',
  answer_version INT UNSIGNED NOT NULL DEFAULT 1 COMMENT 'Per-recipient optimistic revision for stale ballot tabs',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_schedule_poll_members_poll_user (poll_id, user_id),
  UNIQUE KEY uniq_calendar_schedule_poll_members_poll_id_id_user (poll_id, id, user_id),
  KEY idx_calendar_schedule_poll_members_user (user_id, poll_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Snapshot of recipients chosen when a private schedule poll is sent';
