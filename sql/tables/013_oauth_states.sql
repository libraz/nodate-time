CREATE TABLE IF NOT EXISTS oauth_states (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  state_hash  VARCHAR(64)  NOT NULL,
  provider    ENUM('google', 'line') NOT NULL,
  redirect    VARCHAR(512) NOT NULL DEFAULT '',
  expires_at  DATETIME(3)  NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_os_state_hash (state_hash),
  KEY idx_os_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
