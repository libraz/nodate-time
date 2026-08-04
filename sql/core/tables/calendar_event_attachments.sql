-- ====================================
-- calendar_event_attachments
-- Files uploaded against a calendar event. The actual blob and its content
-- metadata (sha256 / byte_size / content_type / storage_key) live in
-- storage_objects; this row is the per-event reference (filename, uploader,
-- sort order). Uses soft-delete via enabled flag so that disabling does not
-- immediately drop the underlying storage_objects ref_count — physical
-- removal happens when the row is hard-deleted.
-- ====================================
CREATE TABLE calendar_event_attachments (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  event_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_events.id; nullable so audit-trail attachments survive event hard-delete (FK SET NULL)',
  uploader_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id (uploader)',
  storage_object_id INT UNSIGNED NOT NULL COMMENT 'FK to storage_objects.id; holds the actual blob metadata (sha256, byte_size, content_type, storage_key)',

  filename VARCHAR(512) NOT NULL COMMENT 'Original filename as supplied by the uploader; widened to 512 to safely hold multibyte paths',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag (soft delete)',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_event_attachments_public_id (public_id),
  UNIQUE KEY uniq_calendar_event_attachments_workspace_public_id (workspace_id, public_id),
  KEY idx_calendar_event_attachments_event (event_id, enabled),
  KEY idx_calendar_event_attachments_workspace_uploader (workspace_id, uploader_id),
  KEY idx_calendar_event_attachments_storage_object (storage_object_id),

  CONSTRAINT fk_calendar_event_attachments_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_attachments_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE SET NULL,
  CONSTRAINT fk_calendar_event_attachments_uploader FOREIGN KEY (uploader_id) REFERENCES users(id) ON DELETE CASCADE,
  -- RESTRICT: attachments must be deleted (and ref_count decremented) before
  -- the underlying storage_objects row may be removed by the GC sweeper.
  CONSTRAINT fk_calendar_event_attachments_storage_object FOREIGN KEY (storage_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar event file attachments';
