-- 19. Notification offsets outside 0..10080, on any row.
-- expect: offset_range 0207
-- expect: offset_range 0208

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, notification_offset)
VALUES
  (207, UNHEX('01900000000070008000000000000207'), 1, 1, 'g19 offset above', 0,
   '2026-08-01 10:00:00', '2026-08-01 11:00:00', 'Asia/Tokyo', 1, 1, 10081),
  (208, UNHEX('01900000000070008000000000000208'), 1, 1, 'g19 offset below', 1,
   '2026-08-01 00:00:00', '2026-08-01 00:00:00', 'Asia/Tokyo', 1, 1, -1);
SET FOREIGN_KEY_CHECKS = 1;
