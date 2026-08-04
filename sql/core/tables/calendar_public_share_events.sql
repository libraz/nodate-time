-- ====================================
-- calendar_public_share_events
-- M:N between calendar_public_shares and calendar_events. Row
-- existence = opt-in for external visibility on that specific share
-- page. One event can appear on multiple shares (e.g. joint concerts
-- listed under two artist pages); removing a row unpublishes without
-- affecting internal visibility.
-- ====================================
CREATE TABLE calendar_public_share_events (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id; denormalized from share for tenant isolation',
  share_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_public_shares.id',
  event_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_events.id',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Override display order on the share page',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag (soft-disable)',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_public_share_events_public_id (public_id),
  UNIQUE KEY uniq_calendar_public_share_events_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendar_public_share_events_share_event (share_id, event_id, enabled) COMMENT 'At most one enabled publication per (share, event)',
  KEY idx_calendar_public_share_events_workspace_event (workspace_id, event_id, enabled),

  CONSTRAINT fk_calendar_public_share_events_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_public_share_events_share FOREIGN KEY (share_id) REFERENCES calendar_public_shares(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_public_share_events_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='M:N: which events appear on which public share pages';
