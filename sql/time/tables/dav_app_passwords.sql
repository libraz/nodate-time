-- ====================================
-- dav_app_passwords
-- Per-device passwords for CalDAV clients, which cannot take part in the
-- browser sign-in flow. Kept apart from the contract's personal_access_tokens:
-- the other product lists, revokes and authenticates with that table as its
-- own API tokens, so a row written here would surface there as a token.
-- A revoked row stays, so its hash can never be issued again.
-- ====================================
CREATE TABLE dav_app_passwords (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  name VARCHAR(100) NOT NULL COMMENT 'Label chosen by the user, usually the device',
  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the password. The plaintext is shown once at creation and never stored.',
  token_prefix CHAR(8) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Leading characters of the plaintext, shown in lists so a user can tell passwords apart',
  last_used_at DATETIME(3) NULL COMMENT 'Last successful authentication; NULL = never used',
  revoked_at DATETIME(3) NULL COMMENT 'Revocation time; a non-NULL value makes the password unusable',

  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_dav_app_passwords_public_id (public_id),
  UNIQUE KEY uniq_dav_app_passwords_token_hash (token_hash),
  KEY idx_dav_app_passwords_user (workspace_id, user_id),

  CONSTRAINT fk_dav_app_passwords_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_dav_app_passwords_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-device CalDAV passwords';
