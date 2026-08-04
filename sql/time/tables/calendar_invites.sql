-- ====================================
-- calendar_invites
-- Join-by-link invitations to a calendar. The contract covers the two
-- neighbouring cases — a workspace invitation, and a read-only public
-- share page — but not this one: a link that grants calendar membership
-- at a stated role. That is this product's model of sharing, so the
-- table lives in this layer.
-- ====================================
CREATE TABLE calendar_invites (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  created_by_user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the invite token. The plaintext is shown once at creation and never stored, so a database read cannot yield a working link.',
  role ENUM('owner','manager','editor','viewer') NOT NULL DEFAULT 'viewer' COMMENT 'Role granted on acceptance. Matches calendar_members.role, and defaults to the least privilege so a link created without naming one cannot hand out more than reading.',
  max_uses INT UNSIGNED NULL COMMENT 'Acceptance limit; NULL = unlimited',
  use_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Acceptances so far',
  expires_at DATETIME(3) NULL COMMENT 'Expiry; NULL = does not expire',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Revocation flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_invites_public_id (public_id),
  UNIQUE KEY uniq_calendar_invites_token_hash (token_hash),
  KEY idx_calendar_invites_calendar (calendar_id, enabled),

  CONSTRAINT fk_calendar_invites_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_invites_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_invites_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE CASCADE,

  CONSTRAINT chk_calendar_invites_uses CHECK (max_uses IS NULL OR use_count <= max_uses)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Join-by-link calendar invitations';
