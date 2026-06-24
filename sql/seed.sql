-- Development sample data: calendars, members, events, and memos.
--
-- Users are NOT created here (no password hashes in version control). The
-- demo/admin accounts are created by the createuser helper first; see the
-- `db-seed` Makefile target. This script resolves them by email, so run it
-- only after those users exist.
SET NAMES utf8mb4;

SET @demo_id  = (SELECT id FROM users WHERE email = 'demo@example.com');
SET @admin_id = (SELECT id FROM users WHERE email = 'admin@example.com');

INSERT IGNORE INTO calendars (id, public_id, name, color, created_by)
VALUES
  (1, UUID_TO_BIN('019da000-0000-7000-8000-000000000010'), 'Work',     '#47B2F7', @demo_id),
  (2, UUID_TO_BIN('019da000-0000-7000-8000-000000000011'), 'Personal', '#2ECC87', @demo_id);

INSERT IGNORE INTO calendar_members (calendar_id, user_id, role, color)
VALUES
  (1, @demo_id,  'admin', '#47B2F7'),
  (2, @demo_id,  'admin', '#2ECC87'),
  (1, @admin_id, 'admin', '#47B2F7'),
  (2, @admin_id, 'admin', '#2ECC87');

-- Sample events for current month
SET @today = CURDATE();
SET @month_start = DATE_FORMAT(@today, '%Y-%m-01');

INSERT IGNORE INTO events (public_id, calendar_id, title, all_day, start_at, end_at, color, location, memo, created_by)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000020'),
   1, 'Team standup', 0,
   CONCAT(@month_start + INTERVAL 1 DAY, ' 10:00:00'),
   CONCAT(@month_start + INTERVAL 1 DAY, ' 10:30:00'),
   '#47B2F7', 'Zoom', '', @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000021'),
   1, 'Sprint review', 0,
   CONCAT(@month_start + INTERVAL 10 DAY, ' 14:00:00'),
   CONCAT(@month_start + INTERVAL 10 DAY, ' 15:00:00'),
   '#B38BDC', 'Meeting Room A', '', @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000022'),
   2, 'Dentist', 0,
   CONCAT(@month_start + INTERVAL 5 DAY, ' 11:00:00'),
   CONCAT(@month_start + INTERVAL 5 DAY, ' 12:00:00'),
   '#F5A623', '', '', @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000023'),
   2, 'Weekend trip', 1,
   CONCAT(@month_start + INTERVAL 14 DAY, ' 00:00:00'),
   CONCAT(@month_start + INTERVAL 16 DAY, ' 00:00:00'),
   '#2ECC87', 'Hakone', 'Pack bags the night before', @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000024'),
   1, 'Release deadline', 1,
   CONCAT(@month_start + INTERVAL 20 DAY, ' 00:00:00'),
   CONCAT(@month_start + INTERVAL 21 DAY, ' 00:00:00'),
   '#E73B3B', '', '', @demo_id);

-- Sample memos
INSERT IGNORE INTO memos (public_id, calendar_id, title, done, sort_order, created_by)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000030'), 1, 'Update project docs', 0, 1, @demo_id),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000031'), 1, 'Review PRs',          1, 2, @demo_id),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000032'), 2, 'Buy groceries',       0, 1, @demo_id);
