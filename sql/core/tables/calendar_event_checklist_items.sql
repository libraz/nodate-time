-- ====================================
-- calendar_event_checklist_items
-- Ordered checklist attached to a calendar event. Each item has a done
-- toggle and sort_weight for drag-and-drop reordering.
-- ====================================
CREATE TABLE calendar_event_checklist_items (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  event_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_events.id',
  created_by_user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  title VARCHAR(500) NOT NULL COMMENT 'Checklist item text',
  done BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Completion flag',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_event_checklist_items_public_id (public_id),
  UNIQUE KEY uniq_calendar_event_checklist_items_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_event_checklist_items_event (workspace_id, event_id, sort_weight),
  KEY idx_calendar_event_checklist_items_creator (workspace_id, created_by_user_id),

  CONSTRAINT fk_calendar_event_checklist_items_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_checklist_items_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_checklist_items_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar event checklist items';
