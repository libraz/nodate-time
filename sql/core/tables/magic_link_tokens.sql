-- ====================================
-- magic_link_tokens
-- Passwordless magic-link authentication tokens. Only the SHA-256 hash
-- of the token is stored; the plaintext URL is sent to the user exactly
-- once via email.
-- ====================================
CREATE TABLE magic_link_tokens (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the token',
  expires_at DATETIME(3) NOT NULL COMMENT 'Token expiry time',
  used_at DATETIME(3) NULL COMMENT 'Time the token was consumed',
  ip_address VARBINARY(16) NULL COMMENT 'Packed IPv4/IPv6 address at creation',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_magic_link_tokens_public_id (public_id),
  UNIQUE KEY uniq_magic_link_tokens_token_hash (token_hash),
  KEY idx_magic_link_tokens_user_id (user_id),
  KEY idx_magic_link_tokens_expires_at (expires_at),

  CONSTRAINT fk_magic_link_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Passwordless magic-link tokens';
