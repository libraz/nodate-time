-- ====================================
-- album_photos
-- Shared photo album pinned to a calendar, optionally to one event.
-- This product's own feature: the contract has no notion of an album,
-- so the table lives in this layer and references core from here.
-- ====================================
CREATE TABLE album_photos (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  calendar_event_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_events.id when the photo was attached to a specific event',
  uploaded_by_user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  caption VARCHAR(500) NOT NULL DEFAULT '' COMMENT 'Free-form caption',
  content_type VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'image/jpeg' COMMENT 'MIME type as uploaded',
  byte_size BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Size in bytes',
  width INT UNSIGNED NULL COMMENT 'Pixel width when known',
  height INT UNSIGNED NULL COMMENT 'Pixel height when known',
  storage_key VARCHAR(1000) NOT NULL COMMENT 'Object key in the blob store',
  storage_object_id INT UNSIGNED NULL COMMENT 'Internal FK to storage_objects.id. NULL until the photo is backfilled; reads fall back to storage_key while it is.',
  thumbnail_object_id INT UNSIGNED NULL COMMENT 'Internal FK to storage_objects.id for the grid-sized rendering. NULL when the photo has none; reads fall back to the photo itself.',
  taken_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'EXIF capture time, falling back to upload time; the album orders by this rather than by upload order',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Soft-delete flag',
  retired_at DATETIME(3) NULL COMMENT 'When a confirmed photo was deleted. NULL on a live photo and on an upload that was never confirmed, which is what keeps a deleted photo from confirming back.',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_album_photos_public_id (public_id),
  UNIQUE KEY uniq_album_photos_workspace_public_id (workspace_id, public_id),
  KEY idx_album_photos_calendar_taken (calendar_id, enabled, taken_at, id),
  KEY idx_album_photos_event (calendar_event_id),
  KEY idx_album_photos_enabled_created (enabled, created_at),

  CONSTRAINT fk_album_photos_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_album_photos_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_album_photos_uploader FOREIGN KEY (uploaded_by_user_id) REFERENCES users(id) ON DELETE CASCADE,
  -- SET NULL: deleting the event a photo was attached to must not delete
  -- the photo. The album outlives the occasion.
  CONSTRAINT fk_album_photos_event FOREIGN KEY (calendar_event_id) REFERENCES calendar_events(id) ON DELETE SET NULL,
  -- RESTRICT: an object a photo still points at must not be deleted out from
  -- under it.
  CONSTRAINT fk_album_photos_storage_object FOREIGN KEY (storage_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT,
  CONSTRAINT fk_album_photos_thumbnail_object FOREIGN KEY (thumbnail_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Shared calendar photo album';
