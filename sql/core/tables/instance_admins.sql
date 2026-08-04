-- ====================================
-- instance_admins
-- Instance-wide admin grants. NOT workspace-scoped: an instance admin can
-- act across every workspace on this deployment.
-- ====================================
CREATE TABLE instance_admins (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',
  granted_by_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id (granter, null for bootstrap)',

  granted_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Time the grant was created',
  revoked_at DATETIME(3) NULL COMMENT 'Explicit revocation time',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_instance_admins_public_id (public_id),
  UNIQUE KEY uniq_instance_admins_user_id (user_id),

  CONSTRAINT fk_instance_admins_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_instance_admins_granted_by FOREIGN KEY (granted_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Instance-wide admin grants';
