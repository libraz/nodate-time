-- Keep / Memo feature (shared to-do lists within a calendar)
CREATE TABLE IF NOT EXISTS memos (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id   BINARY(16)   NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  title       VARCHAR(500) NOT NULL,
  body        TEXT         NOT NULL,
  done        TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order  INT          NOT NULL DEFAULT 0,
  created_by  INT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_memos_public_id (public_id),
  KEY idx_memos_calendar (calendar_id, sort_order),
  CONSTRAINT fk_memos_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_memos_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
