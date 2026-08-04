-- ====================================
-- calendar_subscriptions
-- One user's private display preferences for a calendar: their own colour
-- override and whether the layer is toggled on in their sidebar.
--
-- Not an ACL axis, and deliberately so. Access lives in calendar_members;
-- a row here grants nothing and its absence denies nothing. The split
-- keeps a personal preference — which its own user changes freely — from
-- being a permission write.
-- ====================================
CREATE TABLE calendar_subscriptions (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  display_color VARCHAR(7) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '#4285F4' COMMENT 'Per-subscriber private display color',

  visible BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether this calendar layer is shown in UI',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order in sidebar',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_subscriptions_public_id (public_id),
  UNIQUE KEY uniq_calendar_subscriptions_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendar_subscriptions_calendar_user (calendar_id, user_id),
  KEY idx_calendar_subscriptions_user_workspace (user_id, workspace_id),
  KEY idx_calendar_subscriptions_workspace_calendar (workspace_id, calendar_id),

  CONSTRAINT fk_calendar_subscriptions_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_subscriptions_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_subscriptions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-user display preferences for a calendar (color, visibility). Not an ACL axis — event-level visibility is the only ws-internal ACL.';
