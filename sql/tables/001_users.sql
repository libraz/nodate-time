CREATE TABLE IF NOT EXISTS users (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  name       VARCHAR(100) NOT NULL,
  email      VARCHAR(255) NOT NULL,
  icon       VARCHAR(10)  NOT NULL DEFAULT '👤',
  color      VARCHAR(7)   NOT NULL DEFAULT '#42A5F5',
  avatar_storage_key  VARCHAR(1000) NULL,
  avatar_content_type VARCHAR(255)  NULL,
  password_hash VARCHAR(255) NOT NULL,
  token_version INT UNSIGNED NOT NULL DEFAULT 1,
  password_changed_at DATETIME(3) NULL,
  is_admin   BOOLEAN      NOT NULL DEFAULT FALSE,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_public_id (public_id),
  UNIQUE KEY uk_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
