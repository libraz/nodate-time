-- Development sample data: calendars, members, events, and memos.
--
-- Users are NOT created here (no password hashes in version control). The
-- demo/admin accounts are created by the createuser helper first; see the
-- `db-seed` Makefile target. This script resolves them by email, so run it
-- only after those users exist.
--
-- Every insert is ON DUPLICATE KEY UPDATE id = id rather than INSERT
-- IGNORE, so re-running is still a no-op but a genuine problem -- a user
-- that was never created, a workspace that does not exist -- fails the
-- script instead of being downgraded to a warning that seeds nothing.
SET NAMES utf8mb4;

-- The seeded accounts are created in Asia/Tokyo, so "this month" below means
-- the month it is in Tokyo, not wherever the database server thinks it is.
SET time_zone = '+09:00';
-- Offsets rather than zone names: CONVERT_TZ resolves a name only when the
-- server's time zone tables are loaded, which a stock MySQL image has not.
-- Asia/Tokyo observes no DST, so +09:00 is exact all year.
SET @tz = 'Asia/Tokyo';
SET @tz_offset = '+09:00';

SET @demo_id  = (SELECT id FROM users WHERE email = 'demo@example.com');
SET @admin_id = (SELECT id FROM users WHERE email = 'admin@example.com');

-- Every row below is workspace-scoped. The workspace is read back from the
-- demo account's membership rather than spelled here as a slug: createuser
-- creates whichever workspace TC_WORKSPACE_SLUG names, and a second literal
-- would seed a different one the moment that variable is set.
SET @ws_id = (
  SELECT workspace_id FROM workspace_members
  WHERE user_id = @demo_id AND enabled = TRUE
  ORDER BY id LIMIT 1
);

-- owner_user_id stays NULL, as it does for a calendar created through the
-- API: the owner key cascades, so naming an owner would mean deleting that
-- account takes everyone else's events with it.
INSERT INTO calendars (public_id, workspace_id, kind, name, color)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000010'), @ws_id, 'personal', 'Work',     '#47B2F7'),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000011'), @ws_id, 'personal', 'Personal', '#2ECC87')
ON DUPLICATE KEY UPDATE id = id;

SET @work_cal_id     = (SELECT id FROM calendars WHERE public_id = UUID_TO_BIN('019da000-0000-7000-8000-000000000010'));
SET @personal_cal_id = (SELECT id FROM calendars WHERE public_id = UUID_TO_BIN('019da000-0000-7000-8000-000000000011'));

-- member_color is the colour everyone sees for this member's events, which
-- is why it lives on the membership and not on the account: on a shared
-- calendar the point is telling two people apart.
--
-- Roles are the ones the enum actually has. The demo account owns both
-- calendars; the admin account joins as a manager, which is the delegated
-- role -- it administers membership without being able to delete the
-- calendar out from under the owner.
INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role, member_color)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000040'), @ws_id, @work_cal_id,     @demo_id,  'owner',   '#2ECC87'),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000041'), @ws_id, @personal_cal_id, @demo_id,  'owner',   '#2ECC87'),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000042'), @ws_id, @work_cal_id,     @admin_id, 'manager', '#E73B3B'),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000043'), @ws_id, @personal_cal_id, @admin_id, 'manager', '#E73B3B')
ON DUPLICATE KEY UPDATE id = id;

-- Sample events for the current month.
SET @month_start = DATE_FORMAT(CURDATE(), '%Y-%m-01');

-- start_at and end_at hold UTC instants and the timezone column carries the
-- wall clock they were written in -- the same split the API stores, whose
-- connection is pinned to UTC. The times below therefore read as Tokyo
-- local and are converted once, here.
--
-- An all-day event runs from local midnight to the local midnight after its
-- last day: the stored end is exclusive, so 'Weekend trip' covers two days.
INSERT INTO calendar_events (
  public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
  location, memo, owner_user_id, created_by_user_id
)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000020'),
   @ws_id, @work_cal_id, 'Team standup', 0,
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 1 DAY, '10:00:00'), @tz_offset, '+00:00'),
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 1 DAY, '10:30:00'), @tz_offset, '+00:00'),
   @tz, 'Zoom', '', @demo_id, @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000021'),
   @ws_id, @work_cal_id, 'Sprint review', 0,
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 10 DAY, '14:00:00'), @tz_offset, '+00:00'),
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 10 DAY, '15:00:00'), @tz_offset, '+00:00'),
   @tz, 'Meeting Room A', '', @demo_id, @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000022'),
   @ws_id, @personal_cal_id, 'Dentist', 0,
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 5 DAY, '11:00:00'), @tz_offset, '+00:00'),
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 5 DAY, '12:00:00'), @tz_offset, '+00:00'),
   @tz, '', '', @demo_id, @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000023'),
   @ws_id, @personal_cal_id, 'Weekend trip', 1,
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 14 DAY), @tz_offset, '+00:00'),
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 16 DAY), @tz_offset, '+00:00'),
   @tz, 'Hakone', 'Pack bags the night before', @demo_id, @demo_id),

  (UUID_TO_BIN('019da000-0000-7000-8000-000000000024'),
   @ws_id, @work_cal_id, 'Release deadline', 1,
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 20 DAY), @tz_offset, '+00:00'),
   CONVERT_TZ(TIMESTAMP(@month_start + INTERVAL 21 DAY), @tz_offset, '+00:00'),
   @tz, '', '', @demo_id, @demo_id)
ON DUPLICATE KEY UPDATE id = id;

-- Sample memos
INSERT INTO calendar_memos (public_id, workspace_id, calendar_id, created_by_user_id, title, done, sort_weight)
VALUES
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000030'), @ws_id, @work_cal_id,     @demo_id, 'Update project docs', 0, 1),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000031'), @ws_id, @work_cal_id,     @demo_id, 'Review PRs',          1, 2),
  (UUID_TO_BIN('019da000-0000-7000-8000-000000000032'), @ws_id, @personal_cal_id, @demo_id, 'Buy groceries',       0, 1)
ON DUPLICATE KEY UPDATE id = id;
