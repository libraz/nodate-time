-- 15. An until that is neither a date nor RFC 3339 (a space for the T).
-- expect: until_form 0206

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule)
VALUES
  (206, UNHEX('01900000000070008000000000000206'), 1, 1, 'g15 malformed until', 1,
   '2026-08-01 15:00:00', '2026-08-02 15:00:00', 'Asia/Tokyo', 1, 1, '{"freq":"daily","until":"2026-08-10 00:00:00"}');
SET FOREIGN_KEY_CHECKS = 1;
