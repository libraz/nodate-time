-- 9. An imported row saved since by nf: the nf shape contradicts source_uid.
-- expect: unclassified 0202

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, source_uid)
VALUES
  (202, UNHEX('01900000000070008000000000000202'), 1, 1, 'g9 import saved by nf', 1,
   '2026-08-05 00:00:00', '2026-08-05 00:00:00', 'Asia/Tokyo', 1, 1, 'ical-202@example.com');
SET FOREIGN_KEY_CHECKS = 1;
