-- ====================================
-- workspace_invites
-- Token-based invite links for joining a workspace with a pre-assigned role.
-- The plaintext token is never stored; only its SHA-256 hex hash is persisted.
-- ====================================
CREATE TABLE workspace_invites (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  created_by_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id who created the invite',

  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of invite token plaintext',
  role ENUM('owner','admin','member','guest') NOT NULL DEFAULT 'member' COMMENT 'Role granted on accept',
  max_uses INT UNSIGNED NULL COMMENT 'NULL = unlimited',
  use_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Number of times this invite has been used',
  expires_at DATETIME(3) NULL COMMENT 'NULL = never expires',
  label VARCHAR(255) NULL COMMENT 'Optional human label for the invite link',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_workspace_invites_public_id (public_id),
  UNIQUE KEY uniq_workspace_invites_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_workspace_invites_token_hash (token_hash),
  KEY idx_workspace_invites_workspace_id (workspace_id),
  KEY idx_workspace_invites_expires_at (expires_at),

  CONSTRAINT fk_workspace_invites_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_workspace_invites_created_by FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Token-based workspace invite links';
