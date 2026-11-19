-- Private schedule poll state plus the opt-in source-calendar setting.
-- This upgrade applies to databases already running Nodate Time.

CREATE TABLE IF NOT EXISTS schedule_availability_calendars (
  calendar_id INT UNSIGNED NOT NULL PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  CONSTRAINT fk_schedule_availability_calendars_calendar
    FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Source calendar consent to workspace schedule availability checks';

CREATE TABLE IF NOT EXISTS calendar_schedule_polls (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  public_id BINARY(16) NOT NULL,
  workspace_id INT UNSIGNED NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  created_by_user_id INT UNSIGNED NULL,
  manager_user_id INT UNSIGNED NULL,
  title VARCHAR(500) NOT NULL,
  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL,
  status ENUM('open','finalized','cancelled') NOT NULL DEFAULT 'open',
  version INT UNSIGNED NOT NULL DEFAULT 1,
  response_version INT UNSIGNED NOT NULL DEFAULT 1,
  selected_slot_public_id BINARY(16) NULL,
  confirmed_event_public_id BINARY(16) NULL,
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uniq_calendar_schedule_polls_public_id (public_id),
  UNIQUE KEY uniq_calendar_schedule_polls_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_schedule_polls_calendar_status (workspace_id, calendar_id, status, created_at),
  KEY idx_calendar_schedule_polls_manager (workspace_id, manager_user_id, status),
  KEY idx_calendar_schedule_polls_calendar_id_id (calendar_id, id),
  CONSTRAINT fk_calendar_schedule_polls_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE ON UPDATE RESTRICT,
  CONSTRAINT fk_calendar_schedule_polls_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE ON UPDATE RESTRICT,
  CONSTRAINT fk_calendar_schedule_polls_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL ON UPDATE RESTRICT,
  CONSTRAINT fk_calendar_schedule_polls_manager FOREIGN KEY (manager_user_id) REFERENCES users(id) ON DELETE SET NULL ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private schedule poll lifecycle; only finalized events enter shared history';

CREATE TABLE IF NOT EXISTS calendar_schedule_poll_slots (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  public_id BINARY(16) NOT NULL,
  poll_id INT UNSIGNED NOT NULL,
  start_at DATETIME(3) NOT NULL,
  end_at DATETIME(3) NOT NULL,
  availability ENUM('open','soft','hard') NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uniq_calendar_schedule_poll_slots_public_id (public_id),
  UNIQUE KEY uniq_calendar_schedule_poll_slots_poll_public_id (poll_id, public_id),
  UNIQUE KEY uniq_calendar_schedule_poll_slots_poll_id_id (poll_id, id),
  KEY idx_calendar_schedule_poll_slots_poll_enabled (poll_id, enabled, start_at, id),
  CONSTRAINT fk_calendar_schedule_poll_slots_poll FOREIGN KEY (poll_id) REFERENCES calendar_schedule_polls(id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private schedule poll candidate instants';

CREATE TABLE IF NOT EXISTS calendar_schedule_poll_members (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  poll_id INT UNSIGNED NOT NULL,
  user_id INT UNSIGNED NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  answer_version INT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uniq_calendar_schedule_poll_members_poll_user (poll_id, user_id),
  UNIQUE KEY uniq_calendar_schedule_poll_members_poll_id_id_user (poll_id, id, user_id),
  KEY idx_calendar_schedule_poll_members_user (user_id, poll_id, enabled),
  CONSTRAINT fk_calendar_schedule_poll_members_poll FOREIGN KEY (poll_id) REFERENCES calendar_schedule_polls(id) ON DELETE CASCADE ON UPDATE RESTRICT,
  CONSTRAINT fk_calendar_schedule_poll_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Snapshot of recipients chosen when a private schedule poll is sent';

CREATE TABLE IF NOT EXISTS calendar_schedule_poll_answers (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  poll_id INT UNSIGNED NOT NULL,
  slot_id INT UNSIGNED NOT NULL,
  member_id INT UNSIGNED NOT NULL,
  user_id INT UNSIGNED NOT NULL,
  value ENUM('yes','maybe','no') NOT NULL,
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uniq_calendar_schedule_poll_answers_poll_slot_member (poll_id, slot_id, member_id),
  KEY idx_calendar_schedule_poll_answers_member (poll_id, member_id, slot_id),
  KEY idx_calendar_schedule_poll_answers_user (user_id, poll_id),
  CONSTRAINT fk_calendar_schedule_poll_answers_slot FOREIGN KEY (poll_id, slot_id) REFERENCES calendar_schedule_poll_slots(poll_id, id) ON DELETE CASCADE ON UPDATE RESTRICT,
  CONSTRAINT fk_calendar_schedule_poll_answers_member FOREIGN KEY (poll_id, member_id, user_id) REFERENCES calendar_schedule_poll_members(poll_id, id, user_id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Private yes/maybe/no responses for each recipient and candidate';
