-- ====================================
-- email_verifications
-- Single-use tokens proving a person can read the mailbox they registered
-- with.
--
-- Separate from password_resets for the same reason password_resets is
-- separate from magic_link_tokens: one table would mean a token minted to
-- confirm an address is also accepted to replace a credential.
--
-- What the proof is for: provider sign-in links a verified provider email to
-- an existing account with the same address. Without a proof that the
-- existing account's own address belongs to its owner, anyone can register
-- someone else's address first and inherit the account they later sign in to.
-- ====================================
CREATE TABLE email_verifications (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  email VARCHAR(255) NOT NULL COMMENT 'Address the token was sent to. Recorded so a token stops applying once the account changes address, rather than confirming whatever address is current at redemption time.',
  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the token; the plaintext is emailed once and never stored',
  expires_at DATETIME(3) NOT NULL COMMENT 'Expiry',
  used_at DATETIME(3) NULL COMMENT 'Redemption time; a non-NULL value makes the token spent',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_email_verifications_public_id (public_id),
  UNIQUE KEY uniq_email_verifications_token_hash (token_hash),
  KEY idx_email_verifications_user (user_id, expires_at),
  KEY idx_email_verifications_expiry (expires_at),

  CONSTRAINT fk_email_verifications_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Single-use email ownership proofs';
