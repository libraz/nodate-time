-- ====================================
-- calendar_event_comments
-- Discussion thread on a calendar event. Any calendar member with
-- viewer+ role can comment.
-- ====================================
CREATE TABLE calendar_event_comments (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  event_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_events.id; nullable so audit-trail comments survive event hard-delete (FK SET NULL)',
  author_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  body MEDIUMTEXT NOT NULL COMMENT 'Comment text (markdown)',
  edited_at DATETIME(3) NULL COMMENT 'Last edit time (null = never edited)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  deleted_at DATETIME(3) NULL DEFAULT NULL COMMENT 'Soft-delete timestamp; rows with deleted_at IS NOT NULL are excluded from LIST/GET',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_event_comments_public_id (public_id),
  UNIQUE KEY uniq_calendar_event_comments_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_event_comments_event (workspace_id, event_id, created_at),
  KEY idx_calendar_event_comments_workspace_author (workspace_id, author_id),
  KEY idx_calendar_event_comments_deleted_at (deleted_at),

  CONSTRAINT fk_calendar_event_comments_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_comments_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE SET NULL,
  CONSTRAINT fk_calendar_event_comments_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar event discussion comments';
