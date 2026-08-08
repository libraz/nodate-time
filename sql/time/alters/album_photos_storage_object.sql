-- ====================================
-- album_photos.storage_object_id — put the album on the same blob model as
-- everything else
--
-- Attachments and avatars reach their bytes through storage_objects: one row
-- per (scope, sha256), a ref_count that says whether anything still needs the
-- bytes, and a sweep that reclaims them when nothing does. Album photos alone
-- carried a raw key, so the album is the one place where the question "may
-- these bytes go" is answered by looking at a single row rather than by the
-- count -- which is why deleting a photo could only ever be safe while no two
-- photos could share bytes.
--
-- The column is nullable and storage_key stays: a photo that has not been
-- backfilled yet is read through the old key, so a migration that stops
-- half-way leaves every photo visible. Reads prefer this reference and fall
-- back to storage_key; dropping that column belongs to a later change, once
-- the backfill has been confirmed to have reached every row.
--
-- RESTRICT matches the attachment reference: an object something still points
-- at must not be deleted out from under it. The foreign key's own index is
-- what the backfill scan and the object sweep's exclusion clause both read.
-- ====================================
ALTER TABLE album_photos
  ADD COLUMN storage_object_id INT UNSIGNED NULL
    COMMENT 'Internal FK to storage_objects.id. NULL until the photo is backfilled; reads fall back to storage_key while it is.'
    AFTER storage_key,
  ADD CONSTRAINT fk_album_photos_storage_object
    FOREIGN KEY (storage_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT;
