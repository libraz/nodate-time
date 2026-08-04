-- ====================================
-- instance_settings
-- Instance-level dynamic settings. NOT workspace-scoped: these settings
-- apply globally to the entire deployment.
-- ====================================
CREATE TABLE instance_settings (
  id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id       BINARY(16)     NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  setting_key     VARCHAR(128) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Setting identifier',
  setting_value   TEXT           NOT NULL COMMENT 'Current value as text',
  updated_by_user_id INT UNSIGNED NULL COMMENT 'Last modifier user.id',
  sort_weight     INT            NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes           TEXT           NULL COMMENT 'Admin notes',
  enabled         BOOLEAN        NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at      TIMESTAMP(3)   DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at      DATETIME(3)    NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_instance_settings_public_id (public_id),
  UNIQUE KEY uniq_instance_settings_key (setting_key),

  CONSTRAINT fk_instance_settings_user
    FOREIGN KEY (updated_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Instance-level dynamic settings';
