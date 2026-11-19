CREATE TABLE calendar_schedule_poll_answers (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  poll_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_schedule_polls.id',
  slot_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to a slot in the same poll',
  member_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to a recipient in the same poll',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to the answering user; must match member_id',
  value ENUM('yes','maybe','no') NOT NULL,
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_schedule_poll_answers_poll_slot_member (poll_id, slot_id, member_id),
  KEY idx_calendar_schedule_poll_answers_member (poll_id, member_id, slot_id),
  KEY idx_calendar_schedule_poll_answers_user (user_id, poll_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private yes/maybe/no responses for each recipient and candidate';
