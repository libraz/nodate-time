-- ====================================
-- personal_access_tokens
-- User-scoped API tokens for the REST API and CLI (`tnk`).
-- Only the SHA-256 hash of the bearer token is stored; the plaintext is
-- shown to the user exactly once at creation time.
-- ====================================
CREATE TABLE personal_access_tokens (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id (token owner)',

  name VARCHAR(255) NOT NULL COMMENT 'Human-readable label',
  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the bearer token',
  token_prefix CHAR(8) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Leading chars shown as hint',
  scopes_json JSON NOT NULL COMMENT 'Array of granted API scopes',
  expires_at DATETIME(3) NULL COMMENT 'Expiry time (null = never)',
  last_used_at DATETIME(3) NULL COMMENT 'Last successful use',
  revoked_at DATETIME(3) NULL COMMENT 'Explicit revocation time',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_personal_access_tokens_public_id (public_id),
  UNIQUE KEY uniq_personal_access_tokens_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_personal_access_tokens_token_hash (token_hash),
  KEY idx_personal_access_tokens_workspace_id_user_id (workspace_id, user_id),
  KEY idx_personal_access_tokens_expires_at (expires_at),

  CONSTRAINT fk_personal_access_tokens_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_personal_access_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='User personal access tokens for REST/CLI';
