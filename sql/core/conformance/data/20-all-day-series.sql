-- All-day series carry dates, not instants (PROTOCOL.md §Recurrence
-- exceptions, §Recurrence). A CHECK sees one row at a time, so what spans
-- a series and its overrides is swept here.
--
-- A timed series is not swept: its exception elements may be any of the
-- three readable forms, and an unreadable one is kept on purpose.

SET SESSION group_concat_max_len = 1048576;

-- Every exception element of an all-day series is a YYYY-MM-DD date.
SET @nf_ad_exceptions := (
  SELECT GROUP_CONCAT(DISTINCT CONCAT('event ', ce.id) ORDER BY ce.id SEPARATOR ', ')
  FROM calendar_events ce
  JOIN JSON_TABLE(ce.recurrence_exceptions, '$[*]' COLUMNS (v JSON PATH '$')) jt
  WHERE ce.all_day = TRUE
    AND ce.recurrence_rule IS NOT NULL
    AND JSON_TYPE(ce.recurrence_exceptions) = 'ARRAY'
    AND NOT (JSON_TYPE(jt.v) = 'STRING'
             AND REGEXP_LIKE(JSON_UNQUOTE(jt.v), '^[0-9]{4}-[0-9]{2}-[0-9]{2}$', 'c')));

CALL nf_conformance_assert(
  @nf_ad_exceptions IS NULL,
  CONCAT('20 all-day series with exception elements that are not YYYY-MM-DD dates: ',
         IFNULL(@nf_ad_exceptions, '')));

-- An override of an all-day series replaces an occurrence that starts at
-- UTC midnight, so its recurrence_original_start does too.
SET @nf_ad_overrides := (
  SELECT GROUP_CONCAT(CONCAT('event ', o.id) ORDER BY o.id SEPARATOR ', ')
  FROM calendar_events o
  JOIN calendar_events p ON p.id = o.recurrence_parent_id
  WHERE p.all_day = TRUE
    AND o.recurrence_original_start IS NOT NULL
    AND o.recurrence_original_start <> DATE(o.recurrence_original_start));

CALL nf_conformance_assert(
  @nf_ad_overrides IS NULL,
  CONCAT('20 overrides of all-day series whose recurrence_original_start is not UTC midnight: ',
         IFNULL(@nf_ad_overrides, '')));

SET @nf_ad_exceptions = NULL;
SET @nf_ad_overrides = NULL;
