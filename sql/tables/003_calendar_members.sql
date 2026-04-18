-- Calendar membership with role-based access.
-- role: 'admin' (full control) | 'member' (view + create events) | 'viewer' (read-only)
CREATE TABLE IF NOT EXISTS calendar_members (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id INT UNSIGNED NOT NULL,
  user_id     INT UNSIGNED NOT NULL,
  role        ENUM('admin', 'member', 'viewer') NOT NULL DEFAULT 'member',
  color       VARCHAR(7)   NOT NULL DEFAULT '#42A5F5' COMMENT 'member color within this calendar',
  joined_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_cal_member (calendar_id, user_id),
  CONSTRAINT fk_cm_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_cm_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
