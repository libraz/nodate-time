-- Invitation links for calendars (TimeTree's invite-by-link feature)
CREATE TABLE IF NOT EXISTS calendar_invites (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id INT UNSIGNED NOT NULL,
  token       VARCHAR(64)  NOT NULL COMMENT 'unique invite token',
  role        ENUM('admin', 'member', 'viewer') NOT NULL DEFAULT 'member',
  max_uses    INT UNSIGNED NULL     COMMENT 'null = unlimited',
  use_count   INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at  DATETIME(3)  NULL,
  created_by  INT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_invites_token (token),
  KEY idx_invites_calendar (calendar_id),
  CONSTRAINT fk_invites_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_invites_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
