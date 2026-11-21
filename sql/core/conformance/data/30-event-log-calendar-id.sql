-- A calendar.* log row about an existing event names a calendar
-- (PROTOCOL.md §Log calendar_id). A per-calendar feed reads the log by
-- calendar_id, so a row without one is a change that feed never shows.
-- The value is the calendar at write time, so it is only tested for
-- being present: an event moved since then legitimately differs.

SET SESSION group_concat_max_len = 1048576;

SET @nf_log_no_calendar := (
  SELECT GROUP_CONCAT(DISTINCT e.type ORDER BY e.type SEPARATOR ', ')
  FROM events e
  JOIN calendar_events ce ON ce.public_id = e.subject_public_id
  WHERE e.type LIKE 'calendar.%'
    AND e.calendar_id IS NULL);

CALL nf_conformance_assert(
  @nf_log_no_calendar IS NULL,
  CONCAT('30 calendar.* events rows about an existing event with no calendar_id, kinds: ',
         IFNULL(@nf_log_no_calendar, '')));

SET @nf_log_no_calendar = NULL;
