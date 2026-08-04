-- ====================================
-- oauth_allowed_emails
-- Individual exceptions to the domain restriction on OAuth sign-in.
-- When a domain allow-list is configured, an address listed here may
-- sign in even though its domain is not.
-- ====================================
CREATE TABLE oauth_allowed_emails (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',

  email VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Allowed address, ASCII only to match users.email',
  reason VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Why this address is excepted; the list is unreadable a year later without it',
  created_by_user_id INT UNSIGNED NULL COMMENT 'Operator who added it',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag; FALSE withdraws the exception without losing the record of it',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_oauth_allowed_emails_public_id (public_id),
  UNIQUE KEY uniq_oauth_allowed_emails_email (email),

  CONSTRAINT fk_oauth_allowed_emails_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-address exceptions to the OAuth domain restriction';
