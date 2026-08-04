-- ====================================
-- calendar_memos
-- Shared to-do / memo items pinned to a calendar. TimeTree-compatible
-- feature for lightweight shared checklists that aren't tied to a
-- specific event.
-- ====================================
CREATE TABLE calendar_memos (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  created_by_user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  title VARCHAR(500) NOT NULL COMMENT 'Memo text',
  body TEXT NULL COMMENT 'User-authored multi-line memo body, distinct from admin notes',
  done BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Completion flag',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_memos_public_id (public_id),
  UNIQUE KEY uniq_calendar_memos_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_memos_calendar (calendar_id, sort_weight),
  KEY idx_calendar_memos_workspace (workspace_id, calendar_id),
  KEY idx_calendar_memos_creator (workspace_id, created_by_user_id),

  CONSTRAINT fk_calendar_memos_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_memos_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_memos_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar-level shared memos / to-do items';
