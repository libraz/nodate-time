-- Upgrades a database created by v0.1.0 to the current schema, keeping its data.
--
-- Run it once, against the application database, before starting the new API:
-- the new code reads the columns and the view added here. It does not drop or
-- rewrite anything, and the v0.1.0 API keeps working against the result, so it
-- can be applied while the old version is still serving.

-- A deleted attachment or photo is now told apart from an upload that was never
-- confirmed: both have enabled = FALSE, and retired_at is set only on the
-- deleted one.
ALTER TABLE calendar_event_attachments
  ADD COLUMN retired_at DATETIME(3) NULL
    COMMENT 'When a confirmed attachment was deleted. NULL on a live row and on a reservation that was never confirmed.'
    AFTER enabled;

ALTER TABLE album_photos
  ADD COLUMN retired_at DATETIME(3) NULL
    COMMENT 'When a confirmed photo was deleted. NULL on a live photo and on an upload that was never confirmed, which is what keeps a deleted photo from confirming back.'
    AFTER enabled;

-- Mark the rows v0.1.0 deleted. In v0.1.0 an unconfirmed upload is inserted
-- disabled and never updated until it is confirmed, so a disabled row whose
-- updated_at differs from created_at was confirmed and then deleted. Left
-- unmarked, a deleted row would read as an upload still awaiting confirmation,
-- and confirming it would bring it back. updated_at is kept: the retention of a
-- deleted row is counted from it.
UPDATE calendar_event_attachments
SET retired_at = updated_at, updated_at = updated_at
WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

UPDATE album_photos
SET retired_at = updated_at, updated_at = updated_at
WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

-- Lets the activity feed page one calendar's log by id.
ALTER TABLE events
  ADD KEY idx_events_workspace_calendar_id (workspace_id, calendar_id, id);

-- The objects nothing refers to; the storage sweep deletes only these.
CREATE OR REPLACE VIEW storage_objects_unreferenced AS
SELECT so.id
FROM storage_objects so
WHERE so.ref_count = 0
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
    SELECT 1 FROM users u WHERE u.avatar_storage_object_id = so.id
  );
