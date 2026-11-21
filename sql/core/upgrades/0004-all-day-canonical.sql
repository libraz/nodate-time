-- Brings every all-day calendar_events row to the canonical shape of
-- PROTOCOL.md §All-day events (first day at 00:00 UTC, end exclusive at
-- the next 00:00 UTC, a milestone a point) and then adds
-- chk_calendar_events_all_day_canonical and
-- chk_calendar_events_notification_offset_range.
--
-- Each dated all-day row is classified by its own shape and its provenance
-- as nt (zone midnights, end exclusive), nf (UTC midnights, end inclusive)
-- or canonical; an operator settles any row the rules cannot in
-- core_upgrade_0004_decisions (run sql/core/tools/classify-all-day.sql
-- first). Every stop lists the rows by public_id before it signals. The
-- conversion, the occurrence keys that name its days and the progress mark
-- commit in one transaction, so a re-run after a stop or a crash neither
-- skips nor repeats it. updated_at is kept as it was on every rewritten row.
--
-- Stop both products' writes first. Run with the mysql client, without --force.

SET NAMES utf8mb4;

DROP PROCEDURE IF EXISTS core_upgrade_0004;

DELIMITER //

CREATE PROCEDURE core_upgrade_0004()
BEGIN
  DECLARE v_tz VARCHAR(64) DEFAULT @@SESSION.time_zone;
  DECLARE v_gcml BIGINT UNSIGNED DEFAULT @@SESSION.group_concat_max_len;
  -- The element grammar of PROTOCOL.md §Recurrence exceptions.
  DECLARE v_re_date VARCHAR(64) DEFAULT '^[0-9]{4}-[0-9]{2}-[0-9]{2}$';
  DECLARE v_re_rfc VARCHAR(128) DEFAULT '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$';
  DECLARE v_re_float VARCHAR(64) DEFAULT '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$';
  DECLARE v_sentinel DATETIME(3) DEFAULT '2099-12-31 23:59:59';
  DECLARE v_n INT;
  DECLARE v_msg VARCHAR(128);

  DECLARE EXIT HANDLER FOR SQLEXCEPTION
  BEGIN
    ROLLBACK;
    SET @nf_item_projection_engine = NULL;
    SET SESSION time_zone = v_tz;
    SET SESSION group_concat_max_len = v_gcml;
    RESIGNAL;
  END;

  SET SESSION time_zone = '+00:00';
  SET SESSION group_concat_max_len = 1048576;

  convert_rows: BEGIN
    IF EXISTS (
      SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
      WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
        AND CONSTRAINT_TYPE = 'CHECK'
        AND CONSTRAINT_NAME = 'chk_calendar_events_all_day_canonical'
    ) THEN
      LEAVE convert_rows;
    END IF;

    -- 1. Helper tables. Ordinary rather than TEMPORARY: a temporary table
    --    cannot be opened twice in one statement, and the operator fills
    --    the decision table between runs.
    CREATE TABLE IF NOT EXISTS core_upgrade_0004_decisions (
      public_id BINARY(16) NOT NULL PRIMARY KEY,
      form ENUM('nt','nf','canonical') NOT NULL,
      zone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

    CREATE TABLE IF NOT EXISTS core_upgrade_0004_state (
      step VARCHAR(32) NOT NULL PRIMARY KEY,
      at DATETIME(3) NOT NULL
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

    CREATE TABLE IF NOT EXISTS core_upgrade_0004_class (
      event_id INT UNSIGNED NOT NULL PRIMARY KEY,
      form ENUM('nt','nf','canonical') NOT NULL,
      zone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

    -- The preflight's findings, kept after a stop so they can be read again.
    CREATE TABLE IF NOT EXISTS core_upgrade_0004_stops (
      id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
      reason VARCHAR(32) NOT NULL,
      public_id BINARY(16) NOT NULL,
      detail VARCHAR(255) NULL
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

    IF EXISTS (SELECT 1 FROM core_upgrade_0004_state WHERE step = 'converted') THEN
      LEAVE convert_rows;
    END IF;

    -- 2. Classify every dated all-day row, parents and overrides alike, by
    --    its own shape. A row no rule settles gets no class row.
    DELETE FROM core_upgrade_0004_class;
    INSERT INTO core_upgrade_0004_class (event_id, form, zone)
    SELECT r.id, r.form, r.zone
    FROM (
      SELECT f.id, f.zone,
             CASE
               -- 0. The operator's decision.
               WHEN f.decided IS NOT NULL THEN f.decided
               -- 1-2. One shape fits; provenance may contradict it.
               WHEN f.nt_fit AND NOT f.nf_fit THEN IF(f.task_id IS NULL, 'nt', NULL)
               WHEN f.nf_fit AND NOT f.nt_fit THEN IF(f.source_uid IS NULL, 'nf', NULL)
               -- 3. Both fit (offset 0 on that date); provenance decides.
               WHEN f.nt_fit AND f.nf_fit THEN
                 CASE
                   WHEN f.task_id IS NOT NULL THEN 'nf'
                   WHEN f.source_uid IS NOT NULL THEN 'nt'
                   WHEN f.end_at = f.start_at THEN 'nf'
                 END
             END AS form
      FROM (
        SELECT ce.id, ce.task_id, ce.source_uid, ce.start_at, ce.end_at,
               d.form AS decided,
               COALESCE(d.zone, ce.timezone) AS zone,
               CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone))
                 = DATE(CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone)))
               AND CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone))
                 = DATE(CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone)))
               AND ce.end_at > ce.start_at AS nt_fit,
               ce.start_at = DATE(ce.start_at) AND ce.end_at = DATE(ce.end_at)
               AND ce.end_at >= ce.start_at AS nf_fit
        FROM calendar_events ce
        LEFT JOIN core_upgrade_0004_decisions d ON d.public_id = ce.public_id
        WHERE ce.all_day AND ce.start_at IS NOT NULL
      ) f
    ) r
    WHERE r.form IS NOT NULL;

    -- 3. Preflight. IGNORE keeps an unparseable timestamp a NULL to report
    --    rather than an error that hides the rest of the list.
    DELETE FROM core_upgrade_0004_stops;
    INSERT IGNORE INTO core_upgrade_0004_stops (reason, public_id, detail)
    SELECT s.reason, s.public_id, LEFT(s.detail, 255)
    FROM (
      SELECT 'unclassified' AS reason, ce.public_id,
             CONCAT_WS(' ', ce.kind, ce.timezone, ce.start_at, ce.end_at,
                       IF(ce.task_id IS NULL, NULL, 'task'),
                       IF(ce.source_uid IS NULL, NULL, 'source_uid')) AS detail,
             1 AS ord, ce.id AS row_id
      FROM calendar_events ce
      WHERE ce.all_day AND ce.start_at IS NOT NULL
        AND NOT EXISTS (SELECT 1 FROM core_upgrade_0004_class c WHERE c.event_id = ce.id)

      UNION ALL
      SELECT 'unknown_zone', ce.public_id, COALESCE(d.zone, ce.timezone), 2, ce.id
      FROM calendar_events ce
      LEFT JOIN core_upgrade_0004_decisions d ON d.public_id = ce.public_id
      WHERE ce.all_day AND ce.start_at IS NOT NULL
        AND CONVERT_TZ('2026-01-01 00:00:00', 'UTC', COALESCE(d.zone, ce.timezone)) IS NULL

      UNION ALL
      SELECT 'offset_range', ce.public_id, CAST(ce.notification_offset AS CHAR), 3, ce.id
      FROM calendar_events ce
      WHERE ce.notification_offset < 0 OR ce.notification_offset > 10080

      UNION ALL
      SELECT 'until_form', ce.public_id, JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 4, ce.id
      FROM calendar_events ce
      JOIN core_upgrade_0004_class c ON c.event_id = ce.id
      WHERE JSON_CONTAINS_PATH(ce.recurrence_rule, 'one', '$.until')
        AND NOT (
          JSON_TYPE(JSON_EXTRACT(ce.recurrence_rule, '$.until')) = 'STRING'
          AND (REGEXP_LIKE(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), v_re_date, 'c')
               OR (REGEXP_LIKE(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), v_re_rfc, 'c')
                   AND CONVERT_TZ(
                         CAST(CONCAT(LEFT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 10), ' ',
                                     SUBSTRING(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 12, 8)) AS DATETIME),
                         IF(RIGHT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 1) = 'Z', '+00:00',
                            RIGHT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 6)),
                         '+00:00') IS NOT NULL)))

      -- An occurrence key further than an hour from midnight in the frame
      -- of the series' form names no day with confidence.
      UNION ALL
      SELECT 'exception_key', x.public_id, x.s, 5, x.id
      FROM (
        SELECT ce.id, ce.public_id, e.s,
               CASE
                 WHEN REGEXP_LIKE(e.s, v_re_rfc, 'c') THEN
                   IF(c.form = 'nt',
                      CONVERT_TZ(CONVERT_TZ(CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME),
                                            IF(RIGHT(e.s, 1) = 'Z', '+00:00', RIGHT(e.s, 6)), '+00:00'),
                                 'UTC', c.zone),
                      CONVERT_TZ(CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME),
                                 IF(RIGHT(e.s, 1) = 'Z', '+00:00', RIGHT(e.s, 6)), '+00:00'))
                 ELSE CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME)
               END AS w
        FROM calendar_events ce
        JOIN core_upgrade_0004_class c ON c.event_id = ce.id
        CROSS JOIN JSON_TABLE(ce.recurrence_exceptions, '$[*]' COLUMNS (j JSON PATH '$')) jt
        CROSS JOIN LATERAL (SELECT JSON_UNQUOTE(jt.j) AS s) e
        WHERE JSON_TYPE(ce.recurrence_exceptions) = 'ARRAY'
          AND JSON_LENGTH(ce.recurrence_exceptions) > 0
          AND JSON_TYPE(jt.j) = 'STRING'
          AND (REGEXP_LIKE(e.s, v_re_rfc, 'c') OR REGEXP_LIKE(e.s, v_re_float, 'c'))
      ) x
      WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE)

      UNION ALL
      SELECT 'override_key', x.public_id, CAST(x.ros AS CHAR), 6, x.id
      FROM (
        SELECT o.id, o.public_id, o.recurrence_original_start AS ros,
               IF(c.form = 'nt', CONVERT_TZ(o.recurrence_original_start, 'UTC', c.zone),
                  o.recurrence_original_start) AS w
        FROM calendar_events o
        JOIN core_upgrade_0004_class c ON c.event_id = o.recurrence_parent_id
      ) x
      WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE)

      UNION ALL
      SELECT 'reminder_key', x.public_id, CAST(x.occurrence_start AS CHAR), 7, x.id
      FROM (
        SELECT ce.id, ce.public_id, r.occurrence_start,
               IF(c.form = 'nt', CONVERT_TZ(r.occurrence_start, 'UTC', c.zone), r.occurrence_start) AS w
        FROM calendar_event_reminders r
        JOIN calendar_events ce ON ce.id = r.event_id
        JOIN core_upgrade_0004_class c ON c.event_id = COALESCE(ce.recurrence_parent_id, ce.id)
      ) x
      WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE)

      -- Two keys that round to the same day would break a unique key.
      UNION ALL
      SELECT 'override_collision', x.public_id, CONCAT(HEX(x.parent_public_id), ' ', x.k), 8, x.id
      FROM (
        SELECT o.id, o.public_id, p.public_id AS parent_public_id, k.k,
               COUNT(*) OVER (PARTITION BY o.recurrence_parent_id, k.k) AS n
        FROM calendar_events o
        JOIN core_upgrade_0004_class c ON c.event_id = o.recurrence_parent_id
        JOIN calendar_events p ON p.id = o.recurrence_parent_id
        CROSS JOIN LATERAL (
          SELECT DATE(IF(c.form = 'nt', CONVERT_TZ(o.recurrence_original_start, 'UTC', c.zone),
                         o.recurrence_original_start) + INTERVAL 12 HOUR) AS k
        ) k
      ) x
      WHERE x.n > 1

      UNION ALL
      SELECT 'reminder_collision', x.public_id, CAST(x.k AS CHAR), 9, x.id
      FROM (
        SELECT ce.id, ce.public_id, k.k,
               COUNT(*) OVER (PARTITION BY r.event_id, k.k) AS n
        FROM calendar_event_reminders r
        JOIN calendar_events ce ON ce.id = r.event_id
        JOIN core_upgrade_0004_class c ON c.event_id = COALESCE(ce.recurrence_parent_id, ce.id)
        CROSS JOIN LATERAL (
          SELECT DATE(IF(c.form = 'nt', CONVERT_TZ(r.occurrence_start, 'UTC', c.zone),
                         r.occurrence_start) + INTERVAL 12 HOUR) AS k
        ) k
      ) x
      WHERE x.n > 1
    ) s
    ORDER BY s.ord, s.row_id;

    SELECT COUNT(*) INTO v_n FROM core_upgrade_0004_stops;
    IF v_n > 0 THEN
      SELECT reason AS stop_reason, HEX(public_id) AS public_id_hex, detail
      FROM core_upgrade_0004_stops
      ORDER BY id;

      SET v_msg = LEFT(CONCAT('0004: ', v_n, ' findings need a decision or a fix; see core_upgrade_0004_stops'), 128);
      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = v_msg;
    END IF;

    -- 4. Conversion, with the progress mark in the same transaction.
    START TRANSACTION;
    SET @nf_item_projection_engine = 1;

    -- Both ends in one statement, so the chronology CHECK sees the pair.
    UPDATE calendar_events ce
    JOIN (
      SELECT x.id, x.new_start,
             CASE
               WHEN x.kind = 'milestone' THEN x.new_start
               WHEN x.form = 'nt' THEN DATE(CONVERT_TZ(x.end_at, 'UTC', x.zone))
               WHEN x.form = 'nf' THEN DATE(x.end_at) + INTERVAL 1 DAY
               ELSE DATE(x.end_at)
             END AS new_end
      FROM (
        SELECT ce2.id, ce2.kind, ce2.end_at, c.form, c.zone,
               IF(c.form = 'nt', DATE(CONVERT_TZ(ce2.start_at, 'UTC', c.zone)), DATE(ce2.start_at)) AS new_start
        FROM calendar_events ce2
        JOIN core_upgrade_0004_class c ON c.event_id = ce2.id
      ) x
    ) v ON v.id = ce.id
    SET ce.start_at = v.new_start,
        ce.end_at = v.new_end,
        ce.updated_at = ce.updated_at;

    -- until: a date stays; an RFC 3339 instant becomes its day in the form's frame.
    UPDATE calendar_events ce
    JOIN core_upgrade_0004_class c ON c.event_id = ce.id
    CROSS JOIN LATERAL (
      SELECT CONVERT_TZ(
               CAST(CONCAT(LEFT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 10), ' ',
                           SUBSTRING(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 12, 8)) AS DATETIME),
               IF(RIGHT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 1) = 'Z', '+00:00',
                  RIGHT(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), 6)),
               '+00:00') AS u
    ) t
    SET ce.recurrence_rule = JSON_SET(ce.recurrence_rule, '$.until',
          DATE_FORMAT(IF(c.form = 'nt', DATE(CONVERT_TZ(t.u, 'UTC', c.zone)), DATE(t.u)), '%Y-%m-%d')),
        ce.updated_at = ce.updated_at
    WHERE JSON_TYPE(JSON_EXTRACT(ce.recurrence_rule, '$.until')) = 'STRING'
      AND REGEXP_LIKE(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), v_re_rfc, 'c');

    -- recurrence_end only bounds expansion: one day wider is harmless and
    -- the next edit recomputes it. NULL and the sentinel stay.
    UPDATE calendar_events ce
    JOIN core_upgrade_0004_class c ON c.event_id = ce.id
    SET ce.recurrence_end = LEAST(ce.recurrence_end + INTERVAL 1 DAY, v_sentinel),
        ce.updated_at = ce.updated_at
    WHERE ce.recurrence_end IS NOT NULL
      AND ce.recurrence_end <> v_sentinel;

    -- Exceptions: an RFC 3339 or floating element becomes the day it is
    -- nearest; a date, an unreadable element and a duplicate stay, in order.
    UPDATE calendar_events ce
    JOIN (
      SELECT p.id,
             CAST(CONCAT('[', GROUP_CONCAT(
               CASE
                 WHEN JSON_TYPE(jt.j) = 'STRING' AND REGEXP_LIKE(JSON_UNQUOTE(jt.j), v_re_rfc, 'c') THEN
                   JSON_QUOTE(DATE_FORMAT(DATE(
                     IF(c.form = 'nt',
                        CONVERT_TZ(CONVERT_TZ(CAST(CONCAT(LEFT(JSON_UNQUOTE(jt.j), 10), ' ',
                                                          SUBSTRING(JSON_UNQUOTE(jt.j), 12, 8)) AS DATETIME),
                                              IF(RIGHT(JSON_UNQUOTE(jt.j), 1) = 'Z', '+00:00', RIGHT(JSON_UNQUOTE(jt.j), 6)),
                                              '+00:00'),
                                   'UTC', c.zone),
                        CONVERT_TZ(CAST(CONCAT(LEFT(JSON_UNQUOTE(jt.j), 10), ' ',
                                               SUBSTRING(JSON_UNQUOTE(jt.j), 12, 8)) AS DATETIME),
                                   IF(RIGHT(JSON_UNQUOTE(jt.j), 1) = 'Z', '+00:00', RIGHT(JSON_UNQUOTE(jt.j), 6)),
                                   '+00:00'))
                     + INTERVAL 12 HOUR), '%Y-%m-%d'))
                 WHEN JSON_TYPE(jt.j) = 'STRING' AND REGEXP_LIKE(JSON_UNQUOTE(jt.j), v_re_float, 'c') THEN
                   JSON_QUOTE(DATE_FORMAT(DATE(
                     CAST(CONCAT(LEFT(JSON_UNQUOTE(jt.j), 10), ' ', SUBSTRING(JSON_UNQUOTE(jt.j), 12, 8)) AS DATETIME)
                     + INTERVAL 12 HOUR), '%Y-%m-%d'))
                 ELSE COALESCE(CAST(jt.j AS CHAR), 'null')
               END
               ORDER BY jt.ord SEPARATOR ','), ']') AS JSON) AS arr
      FROM calendar_events p
      JOIN core_upgrade_0004_class c ON c.event_id = p.id
      CROSS JOIN JSON_TABLE(p.recurrence_exceptions, '$[*]' COLUMNS (ord FOR ORDINALITY, j JSON PATH '$')) jt
      WHERE JSON_TYPE(p.recurrence_exceptions) = 'ARRAY'
        AND JSON_LENGTH(p.recurrence_exceptions) > 0
      GROUP BY p.id
    ) v ON v.id = ce.id
    SET ce.recurrence_exceptions = v.arr,
        ce.updated_at = ce.updated_at;

    -- Overrides of an all-day series: the key takes the series' form; the
    -- override's own start and end were converted above by its own class.
    UPDATE calendar_events o
    JOIN core_upgrade_0004_class c ON c.event_id = o.recurrence_parent_id
    SET o.recurrence_original_start = DATE(
          IF(c.form = 'nt', CONVERT_TZ(o.recurrence_original_start, 'UTC', c.zone),
             o.recurrence_original_start) + INTERVAL 12 HOUR),
        o.updated_at = o.updated_at;

    -- Sent-reminder claims follow the series (or the row) they were sent for.
    UPDATE calendar_event_reminders r
    JOIN calendar_events ce ON ce.id = r.event_id
    JOIN core_upgrade_0004_class c ON c.event_id = COALESCE(ce.recurrence_parent_id, ce.id)
    SET r.occurrence_start = DATE(
          IF(c.form = 'nt', CONVERT_TZ(r.occurrence_start, 'UTC', c.zone), r.occurrence_start)
          + INTERVAL 12 HOUR);

    -- PROTOCOL.md §Log calendar_id: a calendar.* row about an existing event.
    UPDATE events e
    JOIN calendar_events ce ON ce.public_id = e.subject_public_id
    SET e.calendar_id = ce.calendar_id,
        e.updated_at = e.updated_at
    WHERE e.type LIKE 'calendar.%'
      AND e.calendar_id IS NULL;

    SELECT COUNT(*) INTO v_n
    FROM calendar_events
    WHERE all_day AND start_at IS NOT NULL
      AND NOT (start_at = DATE(start_at) AND end_at = DATE(end_at) AND (
                (kind = 'milestone' AND end_at = start_at) OR
                (kind <> 'milestone' AND end_at > start_at)));

    IF v_n > 0 THEN
      SELECT HEX(public_id) AS not_canonical_public_id_hex, kind, start_at, end_at
      FROM calendar_events
      WHERE all_day AND start_at IS NOT NULL
        AND NOT (start_at = DATE(start_at) AND end_at = DATE(end_at) AND (
                  (kind = 'milestone' AND end_at = start_at) OR
                  (kind <> 'milestone' AND end_at > start_at)))
      ORDER BY id;

      ROLLBACK;
      SET v_msg = LEFT(CONCAT('0004: ', v_n, ' all-day rows still not canonical after the conversion; nothing was changed'), 128);
      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = v_msg;
    END IF;

    INSERT INTO core_upgrade_0004_state (step, at) VALUES ('converted', NOW(3));
    COMMIT;
    SET @nf_item_projection_engine = NULL;

    IF @core_0004_test_stop_after_convert = 1 THEN
      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '0004: stopped after the conversion (@core_0004_test_stop_after_convert)';
    END IF;
  END convert_rows;

  -- 5. Notification offsets. The preflight refused any row outside the range.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND CONSTRAINT_TYPE = 'CHECK'
      AND CONSTRAINT_NAME = 'chk_calendar_events_notification_offset_range'
  ) THEN
    ALTER TABLE calendar_events ADD
  CONSTRAINT chk_calendar_events_notification_offset_range CHECK (notification_offset IS NULL OR notification_offset BETWEEN 0 AND 10080);
  END IF;

  -- 6. The all-day shape. Byte-identical to sql/core/tables/calendar_events.sql.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND CONSTRAINT_TYPE = 'CHECK'
      AND CONSTRAINT_NAME = 'chk_calendar_events_all_day_canonical'
  ) THEN
    ALTER TABLE calendar_events ADD
  CONSTRAINT chk_calendar_events_all_day_canonical CHECK (
    NOT all_day OR start_at IS NULL OR (
      start_at = DATE(start_at) AND end_at = DATE(end_at) AND (
        (kind = 'milestone' AND end_at = start_at) OR
        (kind <> 'milestone' AND end_at > start_at))));
  END IF;

  -- 7. Column comments, as a fresh load of sql/core/tables/ writes them.
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND COLUMN_NAME = 'visibility'
      AND COLUMN_COMMENT <> 'Who can see event details: default (the calendar''s default_event_visibility), public (every reader), private (members see time and title but not location, memo or url), confidential (only the owner, creator and attendees). PROTOCOL.md §Visibility.'
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN visibility ENUM('default','public','private','confidential') NOT NULL DEFAULT 'default'
        COMMENT 'Who can see event details: default (the calendar''s default_event_visibility), public (every reader), private (members see time and title but not location, memo or url), confidential (only the owner, creator and attendees). PROTOCOL.md §Visibility.';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND COLUMN_NAME = 'start_at'
      AND COLUMN_COMMENT <> 'Start time in UTC; for an all-day row the first day at 00:00:00 UTC; NULL = undated (planning-stage placeholder)'
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN start_at DATETIME(3) NULL
        COMMENT 'Start time in UTC; for an all-day row the first day at 00:00:00 UTC; NULL = undated (planning-stage placeholder)';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND COLUMN_NAME = 'end_at'
      AND COLUMN_COMMENT <> 'End time in UTC; for an all-day row the day after the last day at 00:00:00 UTC (equal to start_at for a milestone); NULL = undated (planning-stage placeholder)'
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN end_at DATETIME(3) NULL
        COMMENT 'End time in UTC; for an all-day row the day after the last day at 00:00:00 UTC (equal to start_at for a milestone); NULL = undated (planning-stage placeholder)';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND COLUMN_NAME = 'recurrence_exceptions'
      AND COLUMN_COMMENT <> 'Array of strings naming occurrences to skip when expanding this rule: YYYY-MM-DD on an all-day series, an RFC 3339 UTC instant on a timed one. Unreadable entries are kept. Cancelling one occurrence is an entry here, never a row. PROTOCOL.md §Recurrence exceptions.'
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN recurrence_exceptions JSON DEFAULT NULL
        COMMENT 'Array of strings naming occurrences to skip when expanding this rule: YYYY-MM-DD on an all-day series, an RFC 3339 UTC instant on a timed one. Unreadable entries are kept. Cancelling one occurrence is an entry here, never a row. PROTOCOL.md §Recurrence exceptions.';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND COLUMN_NAME = 'notification_offset'
      AND COLUMN_COMMENT <> 'Minutes before an occurrence to send its reminder, 0..10080; NULL = no reminder. Applies to every occurrence of a recurring event; what has already been sent is recorded in calendar_event_reminders.'
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN notification_offset INT NULL
        COMMENT 'Minutes before an occurrence to send its reminder, 0..10080; NULL = no reminder. Applies to every occurrence of a recurring event; what has already been sent is recorded in calendar_event_reminders.';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendars' AND COLUMN_NAME = 'default_event_visibility'
      AND COLUMN_COMMENT <> 'What calendar_events.visibility = ''default'' means here. public: details are for every member. private: title and time are visible to members, location, memo and url are for the owner, creator and attendees. Never confidential — hiding the row is a per-event decision.'
  ) THEN
    ALTER TABLE calendars
      MODIFY COLUMN default_event_visibility ENUM('public','private') NOT NULL DEFAULT 'public'
        COMMENT 'What calendar_events.visibility = ''default'' means here. public: details are for every member. private: title and time are visible to members, location, memo and url are for the owner, creator and attendees. Never confidential — hiding the row is a per-event decision.';
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events' AND COLUMN_NAME = 'calendar_id'
      AND COLUMN_COMMENT <> 'Internal FK to calendars.id when the event targets a calendar or something inside one. Non-NULL on every calendar.* row whose subject is an existing event, set to the event''s calendar when the row is written and never rewritten if the event later moves. The symmetric counterpart of task_id, and the reason a per-calendar activity feed can read the log directly instead of keeping a second history table that would drift from it.'
  ) THEN
    ALTER TABLE events
      MODIFY COLUMN calendar_id INT UNSIGNED NULL
        COMMENT 'Internal FK to calendars.id when the event targets a calendar or something inside one. Non-NULL on every calendar.* row whose subject is an existing event, set to the event''s calendar when the row is written and never rewritten if the event later moves. The symmetric counterpart of task_id, and the reason a per-calendar activity feed can read the log directly instead of keeping a second history table that would drift from it.';
  END IF;

  DROP TABLE IF EXISTS core_upgrade_0004_stops;
  DROP TABLE IF EXISTS core_upgrade_0004_class;
  DROP TABLE IF EXISTS core_upgrade_0004_state;
  DROP TABLE IF EXISTS core_upgrade_0004_decisions;

  SET SESSION time_zone = v_tz;
  SET SESSION group_concat_max_len = v_gcml;
END//

DELIMITER ;

CALL core_upgrade_0004();

DROP PROCEDURE core_upgrade_0004;
