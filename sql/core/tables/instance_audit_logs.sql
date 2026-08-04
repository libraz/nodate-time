-- ====================================
-- instance_audit_logs
-- Instance-wide audit trail for operations that cross workspaces or affect
-- the deployment itself (instance admin grants, workspace create/delete,
-- global setting changes). Not workspace-scoped.
-- ====================================
CREATE TABLE instance_audit_logs (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  actor_user_id INT UNSIGNED NULL COMMENT 'Actor user.id (null for system)',

  action VARCHAR(128) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Action identifier (e.g., instance_admin.grant)',
  target_workspace_id INT UNSIGNED NULL COMMENT 'Affected workspace.id when applicable',
  target_resource_type VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'Target resource type',
  target_resource_public_id BINARY(16) NULL COMMENT 'Target resource public_id when available',
  ip_address VARBINARY(16) NULL COMMENT 'Packed IPv4/IPv6 address',
  user_agent VARCHAR(512) NULL COMMENT 'Client user agent',
  payload_json JSON NULL COMMENT 'Redacted payload (no secrets)',
  occurred_at DATETIME(3) NOT NULL COMMENT 'Logical occurrence time (millisecond precision; ties broken by id)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_instance_audit_logs_public_id (public_id),
  KEY idx_instance_audit_logs_occurred_at (occurred_at),
  KEY idx_instance_audit_logs_action (action),
  KEY idx_instance_audit_logs_target_workspace_id (target_workspace_id),

  CONSTRAINT fk_instance_audit_logs_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_instance_audit_logs_workspace FOREIGN KEY (target_workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Instance-wide audit log';
