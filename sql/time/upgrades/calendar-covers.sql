-- Uploaded calendar covers: the link table and the two views that read it.
-- Idempotent; apply after the release file and before triggers.sql.
-- The table is skipped when it already exists, so compare a hand-made one
-- with sql/time/tables/calendar_cover_images.sql first.

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS calendar_cover_images (
  calendar_id INT UNSIGNED NOT NULL PRIMARY KEY,
  storage_object_id INT UNSIGNED NOT NULL,
  CONSTRAINT fk_calendar_cover_images_calendar
    FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_cover_images_object
    FOREIGN KEY (storage_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Uploaded cover images for shared calendars';

CREATE OR REPLACE VIEW v_calendar_cover_images AS
SELECT c.workspace_id, c.id AS calendar_id, so.storage_key
FROM calendar_cover_images ci
JOIN calendars c ON c.id = ci.calendar_id AND c.enabled = TRUE
JOIN storage_objects so ON so.id = ci.storage_object_id AND so.workspace_id = c.workspace_id
WHERE so.enabled = TRUE AND so.uploaded_at IS NOT NULL;

CREATE OR REPLACE VIEW storage_objects_unreferenced AS
SELECT so.id
FROM storage_objects so
WHERE so.ref_count = 0
  AND so.uploaded_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM calendar_event_attachments a WHERE a.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM album_photos ap WHERE ap.thumbnail_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM calendar_cover_images ci WHERE ci.storage_object_id = so.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM users u WHERE u.avatar_storage_object_id = so.id
  );
