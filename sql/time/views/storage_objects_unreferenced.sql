-- ====================================
-- storage_objects_unreferenced — the objects nothing holds
--
-- The one statement of when an object may be deleted: its count is zero and
-- no column that can point at an object does. Both the sweep's listing and
-- every delete read it, so a referring column added later is added once here
-- rather than to each of them.
--
-- Attachments and album photos refer through RESTRICT foreign keys, which
-- would refuse the delete anyway; checking them here keeps a refusal from
-- being attempted and logged every tick. The avatar column is ON DELETE SET
-- NULL, so for it this clause is the only guard: without it a drifted count
-- would clear a live avatar instead of being refused.
-- ====================================
CREATE VIEW storage_objects_unreferenced AS
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
