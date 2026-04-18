CREATE TABLE IF NOT EXISTS event_checklist_items (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  event_id   INT UNSIGNED NOT NULL,
  title      VARCHAR(500) NOT NULL,
  done       TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order INT          NOT NULL DEFAULT 0,
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_ci_pub (public_id),
  KEY idx_ci_event (event_id, sort_order),
  CONSTRAINT fk_ci_event FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_ci_user  FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
