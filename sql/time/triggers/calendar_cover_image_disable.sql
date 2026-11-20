-- Both applications can disable a shared calendar. Release its uploaded
-- cover when that happens, using the same calendar-before-object lock order
-- as the cover API. The normal storage sweep reclaims the unreferenced blob.
DELIMITER $$
CREATE TRIGGER calendar_cover_image_disable
BEFORE UPDATE ON calendars FOR EACH ROW
BEGIN
  IF OLD.enabled AND NOT NEW.enabled THEN
    UPDATE storage_objects so
    INNER JOIN calendar_cover_images ci ON ci.storage_object_id = so.id
    SET so.ref_count = GREATEST(so.ref_count, 1) - 1
    WHERE ci.calendar_id = OLD.id;

    DELETE FROM calendar_cover_images WHERE calendar_id = OLD.id;
  END IF;
END$$
DELIMITER ;
