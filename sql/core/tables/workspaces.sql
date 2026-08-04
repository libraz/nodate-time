-- ====================================
-- workspaces
-- Top-level tenant boundary. Every workspace-scoped table carries
-- workspace_id as its leading composite index column.
-- ====================================
CREATE TABLE workspaces (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',

  slug VARCHAR(63) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'DNS-label slug (RFC 1035)',
  name VARCHAR(255) NOT NULL COMMENT 'Display name',
  description TEXT NULL COMMENT 'Optional description',
  icon_url VARCHAR(2048) NULL COMMENT 'Icon image URL',

  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'UTC' COMMENT 'Default IANA timezone for the workspace; user tz overrides per-user',
  country CHAR(2) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'ISO 3166-1 alpha-2 country; drives default holiday subscription',
  working_days CHAR(7) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'MTWTF__' COMMENT 'Per-day flag string Mon..Sun; letter = working, underscore = off. Default MTWTF__ = Mon-Fri.',
  working_hours_start TIME NOT NULL DEFAULT '09:00:00' COMMENT 'Start of workspace working day (local tz)',
  working_hours_end TIME NOT NULL DEFAULT '18:00:00' COMMENT 'End of workspace working day (local tz)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_workspaces_public_id (public_id),
  UNIQUE KEY uniq_workspaces_slug (slug),
  KEY idx_workspaces_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Tenant boundary';
