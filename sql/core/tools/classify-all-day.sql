-- classify-all-day.sql — what upgrades/0004-all-day-canonical.sql would do
-- with this database's all-day rows, without changing any of them.
--
--   mysql --table ... <db> -e "SET @nf_http_cutoff = '2026-08-05 00:00:00'; source sql/core/tools/classify-all-day.sql"
--
-- @nf_http_cutoff is when nf's HTTP normalisation of all-day rows reached
-- production; the operator supplies it. Before that, nf wrote zone
-- midnights with an inclusive end, which 0004 cannot tell from nt's shape.
--
-- Reports, in order: the time zone tables; zones 0004 cannot resolve; how
-- each row is classified (rule 0-3 and its form, or why it stops); the
-- provenance buckets the rules rest on; every other preflight stop; the
-- two override-attendee populations whose meaning changes with the
-- replace-or-inherit rule; and an INSERT per row that needs an operator
-- decision, to paste into core_upgrade_0004_decisions once the form is
-- filled in. 0004 creates that table on its first run, which stops on
-- those same rows. Decisions already in the table are taken into account.
--
-- Session-scoped only: temporary tables and the session time zone, which
-- is restored at the end.

SET NAMES utf8mb4;
SET @nf_classify_tz = @@SESSION.time_zone;
SET SESSION time_zone = '+00:00';
SET @nf_classify_re_date = '^[0-9]{4}-[0-9]{2}-[0-9]{2}$';
SET @nf_classify_re_rfc = '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$';
SET @nf_classify_re_float = '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$';

-- The operator's decisions, if 0004 has already created the table.
DROP TEMPORARY TABLE IF EXISTS nf_classify_0004_decisions;
CREATE TEMPORARY TABLE nf_classify_0004_decisions (
  public_id BINARY(16) NOT NULL PRIMARY KEY,
  form VARCHAR(9) NOT NULL,
  zone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL
);
SET @nf_classify_q = IF(
  EXISTS (SELECT 1 FROM information_schema.TABLES
          WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'core_upgrade_0004_decisions'),
  'INSERT INTO nf_classify_0004_decisions SELECT public_id, form, zone FROM core_upgrade_0004_decisions',
  'DO 0');
PREPARE nf_classify_stmt FROM @nf_classify_q;
EXECUTE nf_classify_stmt;
DEALLOCATE PREPARE nf_classify_stmt;

-- One row per dated all-day row, with 0004's rules applied.
DROP TEMPORARY TABLE IF EXISTS nf_classify_0004;
CREATE TEMPORARY TABLE nf_classify_0004 AS
SELECT f.*,
       CASE
         WHEN f.decided IS NOT NULL THEN f.decided
         WHEN f.nt_fit AND NOT f.nf_fit AND f.task_id IS NULL THEN 'nt'
         WHEN f.nf_fit AND NOT f.nt_fit AND f.source_uid IS NULL THEN 'nf'
         WHEN f.nt_fit AND f.nf_fit AND f.task_id IS NOT NULL THEN 'nf'
         WHEN f.nt_fit AND f.nf_fit AND f.source_uid IS NOT NULL THEN 'nt'
         WHEN f.nt_fit AND f.nf_fit AND f.end_at = f.start_at THEN 'nf'
       END AS form,
       CONCAT(IF(f.kind = 'milestone' AND f.decided IS NULL, 'rule 1 milestone: ', ''),
         CASE
           WHEN f.decided IS NOT NULL THEN 'rule 0 decision'
           WHEN f.nt_fit IS NULL THEN 'stop: zone unknown'
           WHEN f.nt_fit AND NOT f.nf_fit AND f.task_id IS NOT NULL THEN 'stop: task_id with the nt shape'
           WHEN f.nt_fit AND NOT f.nf_fit THEN 'rule 2 nt shape only'
           WHEN f.nf_fit AND NOT f.nt_fit AND f.source_uid IS NOT NULL THEN 'stop: source_uid with the nf shape'
           WHEN f.nf_fit AND NOT f.nt_fit THEN 'rule 2 nf shape only'
           WHEN f.nt_fit AND f.nf_fit AND f.task_id IS NOT NULL THEN 'rule 3 both shapes, task_id'
           WHEN f.nt_fit AND f.nf_fit AND f.source_uid IS NOT NULL THEN 'rule 3 both shapes, source_uid'
           WHEN f.nt_fit AND f.nf_fit AND f.end_at = f.start_at THEN 'rule 3 both shapes, end = start'
           WHEN f.nt_fit AND f.nf_fit THEN 'stop: both shapes, no provenance'
           ELSE 'stop: neither shape'
         END) AS rule
FROM (
  SELECT ce.id, ce.public_id, ce.kind, ce.timezone, ce.start_at, ce.end_at, ce.task_id, ce.source_uid,
         ce.created_at, ce.recurrence_parent_id, ce.recurrence_rule, ce.recurrence_exceptions,
         d.form AS decided,
         COALESCE(d.zone, ce.timezone) AS zone,
         CONVERT_TZ('2026-01-01 00:00:00', 'UTC', COALESCE(d.zone, ce.timezone)) IS NOT NULL AS zone_ok,
         CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone))
           = DATE(CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone)))
         AND CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone))
           = DATE(CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone))) AS zone_midnights,
         CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone))
           = DATE(CONVERT_TZ(ce.start_at, 'UTC', COALESCE(d.zone, ce.timezone)))
         AND CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone))
           = DATE(CONVERT_TZ(ce.end_at, 'UTC', COALESCE(d.zone, ce.timezone)))
         AND ce.end_at > ce.start_at AS nt_fit,
         ce.start_at = DATE(ce.start_at) AND ce.end_at = DATE(ce.end_at)
         AND ce.end_at >= ce.start_at AS nf_fit
  FROM calendar_events ce
  LEFT JOIN nf_classify_0004_decisions d ON d.public_id = ce.public_id
  WHERE ce.all_day AND ce.start_at IS NOT NULL
) f;

SELECT CONVERT_TZ('2026-01-01 00:00:00', 'UTC', 'Asia/Tokyo') IS NOT NULL AS time_zone_tables_loaded;

SELECT zone AS unresolved_zone, COUNT(*) AS all_day_rows
FROM nf_classify_0004
WHERE NOT zone_ok
GROUP BY zone
ORDER BY zone;

SELECT rule, COALESCE(form, '-') AS form, COUNT(*) AS all_day_rows
FROM nf_classify_0004
GROUP BY rule, form
ORDER BY rule, form;

SELECT 'zone midnights, created before @nf_http_cutoff, no task_id or source_uid' AS bucket,
       IF(@nf_http_cutoff IS NULL, NULL, COUNT(*)) AS all_day_rows,
       IF(@nf_http_cutoff IS NULL, 'set @nf_http_cutoff', '') AS note
FROM nf_classify_0004
WHERE zone_midnights AND end_at >= start_at AND NOT nf_fit
  AND created_at < @nf_http_cutoff AND task_id IS NULL AND source_uid IS NULL;

SELECT 'task_id, not UTC midnights' AS bucket, COUNT(*) AS all_day_rows
FROM nf_classify_0004
WHERE task_id IS NOT NULL AND NOT (start_at = DATE(start_at) AND end_at = DATE(end_at));

SELECT 'source_uid, not the nt shape' AS bucket, COUNT(*) AS all_day_rows
FROM nf_classify_0004
WHERE source_uid IS NOT NULL AND NOT COALESCE(nt_fit, FALSE);

-- The remaining preflight stops of 0004, by the same tests. A temporary
-- table opens once per statement, hence one SELECT each.
SELECT 'notification_offset outside 0..10080' AS stop, COUNT(*) AS findings
FROM calendar_events
WHERE notification_offset < 0 OR notification_offset > 10080;

SELECT 'until neither a date nor RFC 3339' AS stop, COUNT(*) AS findings
FROM nf_classify_0004
WHERE JSON_CONTAINS_PATH(recurrence_rule, 'one', '$.until')
  AND NOT (
    JSON_TYPE(JSON_EXTRACT(recurrence_rule, '$.until')) = 'STRING'
    AND (REGEXP_LIKE(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), @nf_classify_re_date, 'c')
         OR (REGEXP_LIKE(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), @nf_classify_re_rfc, 'c')
             AND CONVERT_TZ(
                   CAST(CONCAT(LEFT(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), 10), ' ',
                               SUBSTRING(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), 12, 8)) AS DATETIME),
                   IF(RIGHT(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), 1) = 'Z', '+00:00',
                      RIGHT(JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, '$.until')), 6)),
                   '+00:00') IS NOT NULL)));

SELECT 'exception element over an hour from midnight, or unparseable' AS stop, COUNT(*) AS findings
FROM (
  SELECT CASE
           WHEN REGEXP_LIKE(e.s, @nf_classify_re_rfc, 'c') THEN
             IF(t.form = 'nt',
                CONVERT_TZ(CONVERT_TZ(CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME),
                                      IF(RIGHT(e.s, 1) = 'Z', '+00:00', RIGHT(e.s, 6)), '+00:00'),
                           'UTC', t.zone),
                CONVERT_TZ(CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME),
                           IF(RIGHT(e.s, 1) = 'Z', '+00:00', RIGHT(e.s, 6)), '+00:00'))
           ELSE CAST(CONCAT(LEFT(e.s, 10), ' ', SUBSTRING(e.s, 12, 8)) AS DATETIME)
         END AS w
  FROM nf_classify_0004 t
  CROSS JOIN JSON_TABLE(t.recurrence_exceptions, '$[*]' COLUMNS (j JSON PATH '$')) jt
  CROSS JOIN LATERAL (SELECT JSON_UNQUOTE(jt.j) AS s) e
  WHERE t.form IS NOT NULL
    AND JSON_TYPE(t.recurrence_exceptions) = 'ARRAY'
    AND JSON_TYPE(jt.j) = 'STRING'
    AND (REGEXP_LIKE(e.s, @nf_classify_re_rfc, 'c') OR REGEXP_LIKE(e.s, @nf_classify_re_float, 'c'))
) x
WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE);

SELECT 'override key over an hour from midnight' AS stop, COUNT(*) AS findings
FROM (
  SELECT IF(t.form = 'nt', CONVERT_TZ(o.recurrence_original_start, 'UTC', t.zone), o.recurrence_original_start) AS w
  FROM calendar_events o
  JOIN nf_classify_0004 t ON t.id = o.recurrence_parent_id
  WHERE t.form IS NOT NULL
) x
WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE);

SELECT 'reminder claim over an hour from midnight' AS stop, COUNT(*) AS findings
FROM (
  SELECT IF(t.form = 'nt', CONVERT_TZ(r.occurrence_start, 'UTC', t.zone), r.occurrence_start) AS w
  FROM calendar_event_reminders r
  JOIN calendar_events ce ON ce.id = r.event_id
  JOIN nf_classify_0004 t ON t.id = COALESCE(ce.recurrence_parent_id, ce.id)
  WHERE t.form IS NOT NULL
) x
WHERE NOT COALESCE(x.w <= DATE(x.w) + INTERVAL 1 HOUR OR x.w >= DATE(x.w) + INTERVAL 23 HOUR, FALSE);

SELECT 'overrides whose keys round to the same day' AS stop, COUNT(*) AS findings
FROM (
  SELECT COUNT(*) OVER (PARTITION BY o.recurrence_parent_id,
           DATE(IF(t.form = 'nt', CONVERT_TZ(o.recurrence_original_start, 'UTC', t.zone),
                   o.recurrence_original_start) + INTERVAL 12 HOUR)) AS n
  FROM calendar_events o
  JOIN nf_classify_0004 t ON t.id = o.recurrence_parent_id
  WHERE t.form IS NOT NULL
) x
WHERE x.n > 1;

SELECT 'reminder claims whose keys round to the same day' AS stop, COUNT(*) AS findings
FROM (
  SELECT COUNT(*) OVER (PARTITION BY r.event_id,
           DATE(IF(t.form = 'nt', CONVERT_TZ(r.occurrence_start, 'UTC', t.zone), r.occurrence_start)
                + INTERVAL 12 HOUR)) AS n
  FROM calendar_event_reminders r
  JOIN calendar_events ce ON ce.id = r.event_id
  JOIN nf_classify_0004 t ON t.id = COALESCE(ce.recurrence_parent_id, ce.id)
  WHERE t.form IS NOT NULL
) x
WHERE x.n > 1;

-- Override attendees under PROTOCOL.md §Override attendees: an override
-- with any attendee row lists the occurrence's attendees in full; one with
-- none inherits the series'. Existing rows are not rewritten, so these
-- live overrides change meaning.
SELECT 'override with attendee rows, missing someone the series has (that person drops out)' AS population,
       COUNT(*) AS overrides
FROM calendar_events o
WHERE o.recurrence_parent_id IS NOT NULL AND o.enabled
  AND EXISTS (SELECT 1 FROM calendar_event_attendees a WHERE a.event_id = o.id)
  AND EXISTS (SELECT 1 FROM calendar_event_attendees pa
              WHERE pa.event_id = o.recurrence_parent_id AND pa.enabled
                AND NOT EXISTS (SELECT 1 FROM calendar_event_attendees a
                                WHERE a.event_id = o.id AND a.user_id = pa.user_id))
UNION ALL
SELECT 'override with no attendee rows under a series that has some (the series attendees return)',
       COUNT(*)
FROM calendar_events o
WHERE o.recurrence_parent_id IS NOT NULL AND o.enabled
  AND NOT EXISTS (SELECT 1 FROM calendar_event_attendees a WHERE a.event_id = o.id)
  AND EXISTS (SELECT 1 FROM calendar_event_attendees pa
              WHERE pa.event_id = o.recurrence_parent_id AND pa.enabled);

-- Rows that need a decision: fill in form (nt, nf or canonical) and, when
-- the row's own timezone is not the frame its midnights were taken in,
-- zone. An empty form is refused by the ENUM, so an unfilled line fails.
SELECT CONCAT('INSERT INTO core_upgrade_0004_decisions (public_id, form, zone) VALUES (UNHEX(''',
              HEX(public_id), '''), '''', NULL); -- ', reason, ': ',
              CONCAT_WS(' ', kind, timezone, start_at, end_at,
                        IF(task_id IS NULL, NULL, 'task'), IF(source_uid IS NULL, NULL, 'source_uid'))) AS decision_line
FROM (
  SELECT t.*, IF(t.form IS NULL, t.rule, 'created before @nf_http_cutoff') AS reason
  FROM nf_classify_0004 t
  WHERE t.form IS NULL
     OR (t.decided IS NULL
         AND t.zone_midnights AND t.end_at >= t.start_at AND NOT t.nf_fit
         AND t.created_at < @nf_http_cutoff AND t.task_id IS NULL AND t.source_uid IS NULL)
) u
ORDER BY id;

DROP TEMPORARY TABLE nf_classify_0004;
DROP TEMPORARY TABLE nf_classify_0004_decisions;
SET SESSION time_zone = @nf_classify_tz;
SET @nf_classify_tz = NULL, @nf_classify_q = NULL,
    @nf_classify_re_date = NULL, @nf_classify_re_rfc = NULL, @nf_classify_re_float = NULL;
