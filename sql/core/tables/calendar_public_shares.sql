-- ====================================
-- calendar_public_shares
-- Workspace-owned publishable read-only pages. Any ws non-guest member
-- can create/edit; delete is admin-only to prevent accidental URL loss.
-- Token is stored as SHA-256 hash; plaintext is returned exactly once
-- at create or rotate.
-- ====================================
CREATE TABLE calendar_public_shares (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  created_by_user_id INT UNSIGNED NULL COMMENT 'Audit trail; ownership is workspace-level so shares survive creator removal',

  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of URL token; plaintext returned once at create/rotate',
  title VARCHAR(255) NOT NULL COMMENT 'Public-facing page title',
  description TEXT NULL COMMENT 'Public-facing description (markdown)',
  icon_url VARCHAR(2048) NULL COMMENT 'Public-facing icon image URL',
  cover_url VARCHAR(2048) NULL COMMENT 'Public-facing cover image URL',

  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'UTC' COMMENT 'Display tz for the public page; defaults to workspace tz at create',
  show_holidays_country CHAR(2) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'ISO 3166-1 alpha-2; NULL = no holiday overlay',
  expires_at DATETIME(3) NULL COMMENT 'NULL = never expires',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order within workspace admin UI',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag (soft-disable)',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_public_shares_public_id (public_id),
  UNIQUE KEY uniq_calendar_public_shares_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendar_public_shares_token_hash (token_hash),
  KEY idx_calendar_public_shares_workspace (workspace_id, enabled),
  KEY idx_calendar_public_shares_expires_at (expires_at),

  CONSTRAINT fk_calendar_public_shares_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_public_shares_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Workspace-owned publishable read-only share pages';
