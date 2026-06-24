-- Individually allow-listed email addresses for OAuth sign-in.
-- Used when a domain restriction (TC_GOOGLE_ALLOWED_DOMAINS) is active: an
-- address listed here may sign in even if its domain is not in the allowed set.
-- Managed from the admin panel.
CREATE TABLE IF NOT EXISTS oauth_allowed_emails (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  email      VARCHAR(255) NOT NULL,
  note       VARCHAR(255) NOT NULL DEFAULT '',
  created_by INT UNSIGNED NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_oae_email (email),
  CONSTRAINT fk_oae_user FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
