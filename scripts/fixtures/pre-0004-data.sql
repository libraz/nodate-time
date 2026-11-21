-- Rows in the shape nt and nf wrote all-day events before the canonical
-- all-day form, loaded by scripts/check-upgrade-path.sh after core upgrades
-- 0001-0003 and before 0004 into every starting state. Each converted row
-- has the value it must hold afterwards in the fx_0004_expect_* tables,
-- which the script reads and drops. The decision rows are what an operator
-- records after classify-all-day.sql, for the two rows no rule settles.
--
-- public_id is 0190000000007000800000000000 followed by the row id in four
-- digits. No task projection is planted: a core + time schema has no task
-- layer and the conformance suite rejects such a row there.

SET NAMES utf8mb4;
SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
SET @nf_item_projection_engine = 1;

-- Present already in the states that loaded v0.1.0-data.sql.
INSERT IGNORE INTO workspaces (id, public_id, slug, name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000001'), 'home', 'Home',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');
INSERT IGNORE INTO users (id, public_id, email, display_name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000a1'), 'owner@example.com', 'Owner',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');
INSERT IGNORE INTO calendars (id, public_id, workspace_id, name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000c1'), 1, 'Family',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000000c2'), 1, 'Club',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, kind, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule, recurrence_end, recurrence_exceptions,
   recurrence_parent_id, recurrence_original_start, notification_offset, task_id, task_role,
   source_uid, enabled, updated_at, created_at)
VALUES
  -- 1. nt, Asia/Tokyo: one day, three days.
  (9101, UUID_TO_BIN(UUID()), 1, 2, 'event', 'g1 nt JST 1 day', 1, '2026-03-09 15:00:00', '2026-03-10 15:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, 10080, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9102, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g1 nt JST 3 days', 1, '2026-03-09 15:00:00', '2026-03-12 15:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 2. nt, America/Los_Angeles across the March DST change.
  (9103, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g2 nt LA across DST', 1, '2026-03-07 08:00:00', '2026-03-10 07:00:00', 'America/Los_Angeles',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 3. nt, Europe/London across the March change: the start is also a UTC midnight.
  (9104, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g3 nt London across March', 1, '2026-03-28 00:00:00', '2026-03-30 23:00:00', 'Europe/London',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 4. nf: one day (end = start), three days.
  (9105, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g4 nf 1 day', 1, '2026-04-01 00:00:00', '2026-04-01 00:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9106, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g4 nf 3 days', 1, '2026-04-01 00:00:00', '2026-04-03 00:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 5. nf with a Los Angeles zone.
  (9107, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g5 nf LA', 1, '2026-05-01 00:00:00', '2026-05-02 00:00:00', 'America/Los_Angeles',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 6. Milestones: an nf point, an nf two-day span.
  (9108, UUID_TO_BIN(UUID()), 1, 1, 'milestone', 'g6 milestone point', 1, '2026-06-01 00:00:00', '2026-06-01 00:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9109, UUID_TO_BIN(UUID()), 1, 1, 'milestone', 'g6 milestone 2 days', 1, '2026-06-01 00:00:00', '2026-06-02 00:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 8. An iCalendar import: UTC midnights, end exclusive.
  (9111, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g8 ical import', 1, '2026-08-01 00:00:00', '2026-08-02 00:00:00', 'UTC',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 'ical-111@example.com', 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 10. UTC zone, no provenance, end exclusive: stops until decided nt.
  (9112, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g10 UTC undecided', 1, '2026-09-01 00:00:00', '2026-09-03 00:00:00', 'UTC',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  -- 11. UTC zone holding Tokyo midnights: stops until decided nt + Asia/Tokyo.
  (9113, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g11 UTC zone JST midnights', 1, '2026-09-09 15:00:00', '2026-09-10 15:00:00', 'UTC',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- 12. Overrides, each row classified by its own shape.
  (9120, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 nf parent', 1, '2026-10-01 00:00:00', '2026-10-01 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":10}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9121, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 nt override of nf parent', 1, '2026-10-03 15:00:00', '2026-10-04 15:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, 9120, '2026-10-03 00:00:00', NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9122, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 nt parent', 1, '2026-10-04 15:00:00', '2026-10-05 15:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":10}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9123, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 nf override of nt parent', 1, '2026-10-08 00:00:00', '2026-10-08 00:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, 9122, '2026-10-06 15:00:00', NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9124, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 timed parent', 0, '2026-10-10 01:00:00', '2026-10-10 02:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":5}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9125, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 all-day override of timed parent', 1, '2026-10-11 15:00:00', '2026-10-12 15:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, 9124, '2026-10-11 01:00:00', NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9126, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 all-day parent of timed override', 1, '2026-10-14 15:00:00', '2026-10-15 15:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":5}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9127, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g12 timed override of all-day parent', 0, '2026-10-16 03:00:00', '2026-10-16 04:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, 9126, '2026-10-16 15:00:00', NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- 13. Keys an hour off midnight, written by an expansion that missed the
  --     Los Angeles summer offset: an override key, an exception, and a
  --     reminder below.
  (9128, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g13 nt LA summer parent', 1, '2026-07-01 07:00:00', '2026-07-02 07:00:00', 'America/Los_Angeles',
   1, 1, '{"freq":"daily","count":10}', NULL, '["2026-07-05T06:00:00Z"]', NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9129, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g13 nf override, key +1h', 1, '2026-07-03 00:00:00', '2026-07-03 00:00:00', 'America/Los_Angeles',
   1, 1, NULL, NULL, NULL, 9128, '2026-07-03 08:00:00', NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- 15. until: a date (nt, LA), RFC 3339 (nt JST, nf, nt LA with an offset).
  (9130, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g15 until date nt LA', 1, '2026-07-20 07:00:00', '2026-07-21 07:00:00', 'America/Los_Angeles',
   1, 1, '{"freq":"daily","until":"2026-07-31"}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9131, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g15 until RFC 3339 nt JST', 1, '2026-08-01 15:00:00', '2026-08-02 15:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","until":"2026-08-10T14:59:59Z"}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9132, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g15 until RFC 3339 nf', 1, '2026-08-01 00:00:00', '2026-08-01 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","until":"2026-08-10T00:00:00Z"}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9133, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g15 until RFC 3339 offset nt LA', 1, '2026-08-10 07:00:00', '2026-08-11 07:00:00', 'America/Los_Angeles',
   1, 1, '{"freq":"daily","until":"2026-08-20T23:59:59-07:00"}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- 16. recurrence_end: the sentinel, NULL, an ordinary value, one a day
  --     short of the sentinel.
  (9134, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g16 recurrence_end sentinel', 1, '2026-11-01 00:00:00', '2026-11-01 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily"}', '2099-12-31 23:59:59', NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9135, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g16 recurrence_end NULL', 1, '2026-11-02 00:00:00', '2026-11-02 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":3}', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9136, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g16 recurrence_end ordinary', 1, '2026-11-03 00:00:00', '2026-11-03 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily","count":3}', '2026-11-05 00:00:00', NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9137, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g16 recurrence_end near sentinel', 1, '2026-11-04 00:00:00', '2026-11-04 00:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily"}', '2099-12-31 10:00:00', NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- 17. Exceptions: empty; ordered elements of every readable form with a
  --     duplicate. (Unreadable elements survive 0004 but fail the data
  --     conformance mode, so they are not planted here.)
  (9138, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g17 exceptions empty', 1, '2026-11-09 15:00:00', '2026-11-10 15:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily"}', NULL, '[]', NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9139, UUID_TO_BIN(UUID()), 1, 1, 'event', 'g17 exceptions ordered', 1, '2026-11-09 15:00:00', '2026-11-10 15:00:00', 'Asia/Tokyo',
   1, 1, '{"freq":"daily"}', NULL,
   '["2026-11-12T15:00:00Z", "2026-11-10", "2026-11-11T00:00:00", "2026-11-14T00:00:00.000+09:00", "2026-11-13"]',
   NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),

  -- A soft-deleted row is converted like a live one; an undated one and a
  -- timed one are left alone.
  (9141, UUID_TO_BIN(UUID()), 1, 1, 'event', 'disabled nt JST', 1, '2026-12-01 15:00:00', '2026-12-02 15:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 0, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9142, UUID_TO_BIN(UUID()), 1, 1, 'event', 'undated all-day', 1, NULL, NULL, 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00'),
  (9143, UUID_TO_BIN(UUID()), 1, 1, 'event', 'timed', 0, '2026-12-05 10:00:00', '2026-12-05 11:00:00', 'Asia/Tokyo',
   1, 1, NULL, NULL, NULL, NULL, NULL, 15, NULL, NULL, NULL, 1, '2020-01-01 00:00:00', '2026-01-01 00:00:00');

UPDATE calendar_events
SET public_id = UNHEX(CONCAT('0190000000007000800000000000', LPAD(id, 4, '0'))),
    updated_at = updated_at
WHERE id BETWEEN 9100 AND 9199;

-- 18. Sent-reminder claims.
INSERT INTO calendar_event_reminders (id, workspace_id, event_id, occurrence_start) VALUES
  (9001, 1, 9101, '2026-03-09 15:00:00'),
  (9002, 1, 9128, '2026-07-02 06:00:00'),
  (9003, 1, 9121, '2026-10-03 00:00:00'),
  (9004, 1, 9124, '2026-10-10 01:00:00'),
  (9005, 1, 9122, '2026-10-04 15:00:00'),
  (9006, 1, 9127, '2026-10-16 15:00:00');

-- 21. Log rows: calendar_id NULL on calendar.* about an existing event is
--     filled; a non-calendar kind and a value already set are left.
INSERT INTO events (id, public_id, workspace_id, calendar_id, subject_public_id, type, payload_json, occurred_at, updated_at) VALUES
  (9001, UNHEX('01900000000070008000000000e00001'), 1, NULL, UNHEX('01900000000070008000000000009101'), 'calendar.event.created', '{"eventId":"x","title":"t"}', '2026-01-01 00:00:00', '2020-01-01 00:00:00'),
  (9003, UNHEX('01900000000070008000000000e00003'), 1, NULL, UNHEX('01900000000070008000000000009101'), 'task.created', '{}', '2026-01-01 00:00:00', '2020-01-01 00:00:00'),
  (9004, UNHEX('01900000000070008000000000e00004'), 1, 1, UNHEX('01900000000070008000000000009101'), 'calendar.event.created', '{"eventId":"x","title":"t"}', '2026-01-01 00:00:00', '2020-01-01 00:00:00'),
  (9005, UNHEX('01900000000070008000000000e00005'), 1, NULL, UNHEX('01900000000070008000000000009141'), 'calendar.event.deleted', '{"eventId":"x","title":"t"}', '2026-01-01 00:00:00', '2020-01-01 00:00:00');

-- Decisions for the rows no rule settles: a UTC-zone row with no provenance
-- (nt) and a UTC-zone row holding Tokyo midnights (nt in Asia/Tokyo).
CREATE TABLE IF NOT EXISTS core_upgrade_0004_decisions (
  public_id BINARY(16) NOT NULL PRIMARY KEY,
  form ENUM('nt','nf','canonical') NOT NULL,
  zone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO core_upgrade_0004_decisions (public_id, form, zone) VALUES
  (UNHEX('01900000000070008000000000009112'), 'nt', NULL),
  (UNHEX('01900000000070008000000000009113'), 'nt', 'Asia/Tokyo');

SET @nf_item_projection_engine = NULL;
SET FOREIGN_KEY_CHECKS = 1;

-- Expected values after 0004. until_v holds the unquoted string, exceptions
-- the array as JSON; every converted row keeps updated_at.
CREATE TABLE fx_0004_expect_event (
  id INT UNSIGNED PRIMARY KEY,
  start_at DATETIME(3) NULL,
  end_at DATETIME(3) NULL,
  until_v VARCHAR(64) NULL,
  recurrence_end DATETIME(3) NULL,
  exceptions JSON NULL,
  original_start DATETIME(3) NULL
);

INSERT INTO fx_0004_expect_event (id, start_at, end_at, until_v, recurrence_end, exceptions, original_start) VALUES
  (9101, '2026-03-10', '2026-03-11', NULL, NULL, NULL, NULL),
  (9102, '2026-03-10', '2026-03-13', NULL, NULL, NULL, NULL),
  (9103, '2026-03-07', '2026-03-10', NULL, NULL, NULL, NULL),
  (9104, '2026-03-28', '2026-03-31', NULL, NULL, NULL, NULL),
  (9105, '2026-04-01', '2026-04-02', NULL, NULL, NULL, NULL),
  (9106, '2026-04-01', '2026-04-04', NULL, NULL, NULL, NULL),
  (9107, '2026-05-01', '2026-05-03', NULL, NULL, NULL, NULL),
  (9108, '2026-06-01', '2026-06-01', NULL, NULL, NULL, NULL),
  (9109, '2026-06-01', '2026-06-01', NULL, NULL, NULL, NULL),
  (9111, '2026-08-01', '2026-08-02', NULL, NULL, NULL, NULL),
  (9112, '2026-09-01', '2026-09-03', NULL, NULL, NULL, NULL),
  (9113, '2026-09-10', '2026-09-11', NULL, NULL, NULL, NULL),
  (9120, '2026-10-01', '2026-10-02', NULL, NULL, NULL, NULL),
  (9121, '2026-10-04', '2026-10-05', NULL, NULL, NULL, '2026-10-03 00:00:00'),
  (9122, '2026-10-05', '2026-10-06', NULL, NULL, NULL, NULL),
  (9123, '2026-10-08', '2026-10-09', NULL, NULL, NULL, '2026-10-07 00:00:00'),
  (9124, '2026-10-10 01:00:00', '2026-10-10 02:00:00', NULL, NULL, NULL, NULL),
  (9125, '2026-10-12', '2026-10-13', NULL, NULL, NULL, '2026-10-11 01:00:00'),
  (9126, '2026-10-15', '2026-10-16', NULL, NULL, NULL, NULL),
  (9127, '2026-10-16 03:00:00', '2026-10-16 04:00:00', NULL, NULL, NULL, '2026-10-17 00:00:00'),
  (9128, '2026-07-01', '2026-07-02', NULL, NULL, '["2026-07-05"]', NULL),
  (9129, '2026-07-03', '2026-07-04', NULL, NULL, NULL, '2026-07-03 00:00:00'),
  (9130, '2026-07-20', '2026-07-21', '2026-07-31', NULL, NULL, NULL),
  (9131, '2026-08-02', '2026-08-03', '2026-08-10', NULL, NULL, NULL),
  (9132, '2026-08-01', '2026-08-02', '2026-08-10', NULL, NULL, NULL),
  (9133, '2026-08-10', '2026-08-11', '2026-08-20', NULL, NULL, NULL),
  (9134, '2026-11-01', '2026-11-02', NULL, '2099-12-31 23:59:59', NULL, NULL),
  (9135, '2026-11-02', '2026-11-03', NULL, NULL, NULL, NULL),
  (9136, '2026-11-03', '2026-11-04', NULL, '2026-11-06 00:00:00', NULL, NULL),
  (9137, '2026-11-04', '2026-11-05', NULL, '2099-12-31 23:59:59', NULL, NULL),
  (9138, '2026-11-10', '2026-11-11', NULL, NULL, '[]', NULL),
  (9139, '2026-11-10', '2026-11-11', NULL, NULL, '["2026-11-13", "2026-11-10", "2026-11-11", "2026-11-14", "2026-11-13"]', NULL),
  (9141, '2026-12-02', '2026-12-03', NULL, NULL, NULL, NULL),
  (9142, NULL, NULL, NULL, NULL, NULL, NULL),
  (9143, '2026-12-05 10:00:00', '2026-12-05 11:00:00', NULL, NULL, NULL, NULL);

CREATE TABLE fx_0004_expect_reminder (
  id INT UNSIGNED PRIMARY KEY,
  occurrence_start DATETIME(3) NOT NULL
);

INSERT INTO fx_0004_expect_reminder (id, occurrence_start) VALUES
  (9001, '2026-03-10 00:00:00'),
  (9002, '2026-07-02 00:00:00'),
  (9003, '2026-10-03 00:00:00'),
  (9004, '2026-10-10 01:00:00'),
  (9005, '2026-10-05 00:00:00'),
  (9006, '2026-10-17 00:00:00');

CREATE TABLE fx_0004_expect_log (
  id BIGINT UNSIGNED PRIMARY KEY,
  calendar_id INT UNSIGNED NULL
);

INSERT INTO fx_0004_expect_log (id, calendar_id) VALUES
  (9001, 2),
  (9003, NULL),
  (9004, 1),
  (9005, 1);
