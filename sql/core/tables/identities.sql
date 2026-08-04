-- ====================================
-- identities
-- Authentication credentials for users. A user can have multiple
-- identities: one local (password) plus zero or more OIDC providers.
-- ====================================
CREATE TABLE identities (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  provider ENUM('local','google','github','microsoft','generic_oidc') NOT NULL COMMENT 'Identity provider kind',
  subject VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Provider subject / external ID (ASCII)',
  password_hash CHAR(97) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'Argon2id encoded hash, only for provider=local', -- argon2id encoded form (97 chars)
  mfa_secret_ciphertext VARBINARY(512) NULL COMMENT 'Encrypted TOTP secret (AES-256-GCM)',
  mfa_confirmed_at DATETIME(3) NULL COMMENT 'When the TOTP enrollment was confirmed by submitting a valid code',
  mfa_last_step BIGINT UNSIGNED NULL COMMENT 'Last accepted TOTP time-step (unix/period). RFC 6238 5.2 one-time-use: a code whose step is <= this value is rejected as a replay',
  failed_attempts INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Consecutive failed login attempts',
  locked_until_at DATETIME(3) NULL COMMENT 'Lockout expiry timestamp',
  last_used_at DATETIME(3) NULL COMMENT 'Last successful authentication time',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_identities_public_id (public_id),
  UNIQUE KEY uniq_identities_provider_subject (provider, subject),
  KEY idx_identities_user_id (user_id),

  CONSTRAINT fk_identities_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='User authentication identities';
