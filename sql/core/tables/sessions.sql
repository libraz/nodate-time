-- ====================================
-- sessions
-- Refresh-token backed user sessions. Access tokens are stateless JWT;
-- this table only tracks the refresh token hash and its metadata.
-- ====================================
CREATE TABLE sessions (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  refresh_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of refresh token',
  user_agent VARCHAR(512) NULL COMMENT 'Client user agent at issue time',
  ip_address VARBINARY(16) NULL COMMENT 'Packed IPv4/IPv6 address at issue time',
  expires_at DATETIME(3) NOT NULL COMMENT 'Refresh token expiry',
  revoked_at DATETIME(3) NULL COMMENT 'Explicit revocation time',
  last_used_at DATETIME(3) NULL COMMENT 'Last refresh time',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_sessions_public_id (public_id),
  UNIQUE KEY uniq_sessions_refresh_hash (refresh_hash),
  KEY idx_sessions_user_id_expires_at (user_id, expires_at),

  CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Refresh-token backed sessions';
