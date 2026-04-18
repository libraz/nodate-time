CREATE TABLE IF NOT EXISTS event_attachments (
  id           INT UNSIGNED  NOT NULL AUTO_INCREMENT,
  public_id    BINARY(16)    NOT NULL,
  event_id     INT UNSIGNED  NOT NULL,
  uploaded_by  INT UNSIGNED  NOT NULL,
  filename     VARCHAR(500)  NOT NULL,
  content_type VARCHAR(255)  NOT NULL DEFAULT 'application/octet-stream',
  byte_size    BIGINT        NOT NULL DEFAULT 0,
  storage_key  VARCHAR(1000) NOT NULL COMMENT 'MinIO object key',
  enabled      TINYINT(1)    NOT NULL DEFAULT 1 COMMENT 'soft delete flag',
  created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_att_pub (public_id),
  KEY idx_att_event (event_id, enabled),
  CONSTRAINT fk_att_event FOREIGN KEY (event_id)    REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_att_user  FOREIGN KEY (uploaded_by)  REFERENCES users  (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
