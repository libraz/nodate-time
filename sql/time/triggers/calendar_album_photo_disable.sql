-- Both applications can disable a shared calendar. Retire its album photos
-- with it; their references stay held until the cleanup sweep releases them.
-- The UPDATE already holds the calendar row, so the photos lock after it, the
-- same order as confirming a photo.
DELIMITER $$
CREATE TRIGGER calendar_album_photo_disable
BEFORE UPDATE ON calendars FOR EACH ROW
BEGIN
  IF OLD.enabled AND NOT NEW.enabled THEN
    UPDATE album_photos SET enabled = FALSE, retired_at = CURRENT_TIMESTAMP(3)
    WHERE calendar_id = OLD.id AND enabled = TRUE;
  END IF;
END$$
DELIMITER ;
