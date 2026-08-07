-- ====================================
-- password_resets
-- Single-use password reset tokens.
--
-- Not magic_link_tokens, which the contract already defines: that token
-- signs someone in, this one lets them replace a credential. Sharing one
-- table would mean a token minted for either purpose is accepted for
-- both, and a reset link leaked from an inbox would become a login.
-- ====================================
CREATE TABLE password_resets (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the token; the plaintext is emailed once and never stored',
  expires_at DATETIME(3) NOT NULL COMMENT 'Expiry',
  used_at DATETIME(3) NULL COMMENT 'Redemption time; a non-NULL value makes the token spent',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_password_resets_public_id (public_id),
  UNIQUE KEY uniq_password_resets_token_hash (token_hash),
  KEY idx_password_resets_user (user_id, expires_at),
  -- The expiry sweep names no user, and idx_password_resets_user cannot be
  -- entered from its second column, so that lookup needs its own index.
  KEY idx_password_resets_expires_at (expires_at),

  CONSTRAINT fk_password_resets_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Single-use password reset tokens';
