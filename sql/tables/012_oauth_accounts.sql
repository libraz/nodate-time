CREATE TABLE IF NOT EXISTS oauth_accounts (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id     INT UNSIGNED NOT NULL,
  provider    ENUM('google', 'line') NOT NULL,
  provider_subject VARCHAR(255) NOT NULL COMMENT 'sub claim or LINE userId',
  email       VARCHAR(255) NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_oa_provider_subject (provider, provider_subject),
  KEY idx_oa_user (user_id),
  CONSTRAINT fk_oa_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
