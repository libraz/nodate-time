-- ====================================
-- audit_logs
-- Workspace-scoped audit trail for sensitive actions (auth, ACL, secret
-- rotation, MCP tool writes). Values are stored post-redaction.
-- ====================================
CREATE TABLE audit_logs (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  actor_user_id INT UNSIGNED NULL COMMENT 'Actor user.id (null for system)',

  action VARCHAR(128) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Action identifier (e.g., ai_provider.create)',
  resource_type VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Target resource type',
  resource_public_id BINARY(16) NULL COMMENT 'Target resource public_id when available',
  ip_address VARBINARY(16) NULL COMMENT 'Packed IPv4/IPv6 address',
  user_agent VARCHAR(512) NULL COMMENT 'Client user agent',
  metadata_json JSON NULL COMMENT 'Redacted metadata (no secrets, no raw keys)',
  occurred_at DATETIME(3) NOT NULL COMMENT 'Logical occurrence time (millisecond precision; ties broken by id)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_audit_logs_public_id (public_id),
  UNIQUE KEY uniq_audit_logs_workspace_public_id (workspace_id, public_id),
  KEY idx_audit_logs_workspace_id_occurred_at (workspace_id, occurred_at),
  KEY idx_audit_logs_workspace_id_action (workspace_id, action),

  CONSTRAINT fk_audit_logs_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_audit_logs_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Workspace audit log';
