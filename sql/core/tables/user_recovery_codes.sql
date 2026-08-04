-- ====================================
-- user_recovery_codes
-- One-time recovery codes used to bypass TOTP at login when the user
-- loses access to their authenticator app. Codes are stored as
-- SHA-256 hashes; the plaintext is shown to the user only once at
-- generation time.
-- ====================================
CREATE TABLE user_recovery_codes (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',
  code_hash BINARY(32) NOT NULL COMMENT 'SHA-256 of the normalized recovery code',
  used_at DATETIME(3) NULL COMMENT 'Set when the code is consumed at login',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_user_recovery_codes_user_hash (user_id, code_hash),
  KEY idx_user_recovery_codes_user_used (user_id, used_at),

  CONSTRAINT fk_user_recovery_codes_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='TOTP recovery codes';
