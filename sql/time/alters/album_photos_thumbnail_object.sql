-- ====================================
-- album_photos.thumbnail_object_id — a second, small rendering of a photo
--
-- The album grid draws tiles about 134px wide and was drawing them with the
-- stored photo, which is up to 2048px on its longest edge. Thirty photos is
-- tens of megabytes to show a three-column grid, and lazy loading only spreads
-- that over scrolling rather than avoiding it.
--
-- The thumbnail is a second reference into storage_objects rather than a
-- second raw key, so it is reclaimed by the same reference count and the same
-- sweep as everything else. That ordering is deliberate: putting a second key
-- on this table would have made the album the one place holding blobs the
-- object model does not know about.
--
-- Nullable, and it stays nullable. A photo may have no thumbnail for reasons
-- that are not failures: an animated GIF is never re-encoded, because a
-- thumbnail drawn from a canvas is its first frame and the grid would stop
-- animating; a photo already smaller than a tile has nothing to gain from one;
-- and a thumbnail upload that does not arrive must not cost the photo itself.
-- Every read falls back to the photo, which is correct, just larger.
--
-- RESTRICT matches the other two references into storage_objects. Note that
-- the object sweep excludes referrers column by column, so this column has to
-- be named there as well -- an object kept alive only as a thumbnail is
-- otherwise collectable, and the tile it was drawing goes blank with nothing
-- reporting an error.
-- ====================================
ALTER TABLE album_photos
  ADD COLUMN thumbnail_object_id INT UNSIGNED NULL
    COMMENT 'Internal FK to storage_objects.id for the grid-sized rendering. NULL when the photo has none; reads fall back to the photo itself.'
    AFTER storage_object_id,
  ADD CONSTRAINT fk_album_photos_thumbnail_object
    FOREIGN KEY (thumbnail_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT;
