-- 14. Two overrides whose keys round to the same day of an nf series.
-- expect: override_collision 0204
-- expect: override_collision 0205

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule, recurrence_parent_id, recurrence_original_start)
VALUES
  (203, UNHEX('01900000000070008000000000000203'), 1, 1, 'g14 nf parent', 1,
   '2026-11-01 00:00:00', '2026-11-01 00:00:00', 'Asia/Tokyo', 1, 1, '{"freq":"daily"}', NULL, NULL),
  (204, UNHEX('01900000000070008000000000000204'), 1, 1, 'g14 override at midnight', 0,
   '2026-11-01 05:00:00', '2026-11-01 06:00:00', 'Asia/Tokyo', 1, 1, NULL, 203, '2026-11-01 00:00:00'),
  (205, UNHEX('01900000000070008000000000000205'), 1, 1, 'g14 override half an hour later', 0,
   '2026-11-01 07:00:00', '2026-11-01 08:00:00', 'Asia/Tokyo', 1, 1, NULL, 203, '2026-11-01 00:30:00');
SET FOREIGN_KEY_CHECKS = 1;
