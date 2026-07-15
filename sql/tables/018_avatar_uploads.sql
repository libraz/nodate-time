CREATE TABLE IF NOT EXISTS avatar_uploads (
  id           INT UNSIGNED  NOT NULL AUTO_INCREMENT,
  public_id    BINARY(16)    NOT NULL,
  user_id      INT UNSIGNED  NOT NULL,
  storage_key  VARCHAR(1000) NOT NULL,
  content_type VARCHAR(255)  NOT NULL,
  byte_size    BIGINT        NOT NULL,
  expires_at   DATETIME(3)   NOT NULL,
  created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_avatar_upload_public_id (public_id),
  KEY idx_avatar_upload_user_expiry (user_id, expires_at),
  KEY idx_avatar_upload_expiry (expires_at),
  CONSTRAINT fk_avatar_upload_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
