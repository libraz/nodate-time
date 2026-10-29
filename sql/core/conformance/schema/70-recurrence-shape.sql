-- The shape of calendar_events.recurrence_rule, held by
-- chk_calendar_events_recurrence_shape (PROTOCOL.md §Recurrence).
--
-- MySQL enforces the draft-4 keywords of JSON_SCHEMA_VALID and silently
-- ignores newer ones, so a schema can read as strict while enforcing
-- nothing. Each refused rule below differs from a valid one in a single
-- keyword, which makes every keyword the schema relies on answer for
-- itself. The accepted rules are the other half: values at the edge of a
-- range, and shapes the CHECK deliberately leaves to each writer's own
-- validation, so an over-strict CHECK fails here too.
--
-- nf_conformance_expect_recurrence insists the refusal names this CHECK;
-- a rule refused by some other constraint does not count.

-- type: the rule is an object, and integers are integers. 1.0 is a
-- number but not an integer to MySQL, and a writer emitting it is wrong.
CALL nf_conformance_expect_recurrence('["daily"]', FALSE, '70 type: a rule that is not an object');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":1.5}', FALSE, '70 type: interval 1.5');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":1.0}', FALSE, '70 type: interval 1.0');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":true}', FALSE, '70 type: interval true');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":null}', FALSE, '70 type: interval null');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":5}', FALSE, '70 type: scalar byMonthDay');
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":20260304}', FALSE, '70 type: numeric until');

-- required and additionalProperties: freq is the one mandatory key, and
-- nothing outside the listed keys is stored.
CALL nf_conformance_expect_recurrence('{"interval":1}', FALSE, '70 required: no freq');
CALL nf_conformance_expect_recurrence('{"freq":"daily","byHour":[9]}', FALSE, '70 additionalProperties: unknown key');

-- enum: values are lower-case frequencies and upper-case two-letter days.
CALL nf_conformance_expect_recurrence('{"freq":"hourly"}', FALSE, '70 enum: freq hourly');
CALL nf_conformance_expect_recurrence('{"freq":"WEEKLY"}', FALSE, '70 enum: freq WEEKLY');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","byDay":["mo"]}', FALSE, '70 enum: byDay mo');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","wkst":"TU"}', FALSE, '70 enum: wkst TU');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["MO"],"bySetPos":[0]}', FALSE, '70 enum: bySetPos 0');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["MO"],"bySetPos":[6]}', FALSE, '70 enum: bySetPos 6');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["MO"],"bySetPos":[-2]}', FALSE, '70 enum: bySetPos -2');

-- minimum and maximum.
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":0}', FALSE, '70 minimum: interval 0');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":-1}', FALSE, '70 minimum: interval -1');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":1000}', FALSE, '70 maximum: interval 1000');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[0]}', FALSE, '70 minimum: byMonthDay 0');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[-1]}', FALSE, '70 minimum: byMonthDay -1');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[32]}', FALSE, '70 maximum: byMonthDay 32');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":0}', FALSE, '70 minimum: count 0');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":1001}', FALSE, '70 maximum: count 1001');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":2147483648}', FALSE, '70 maximum: count 2^31');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":4294967296}', FALSE, '70 maximum: count 2^32');

-- minItems, maxItems and uniqueItems.
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[]}', FALSE, '70 minItems: empty byMonthDay');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","byDay":[]}', FALSE, '70 minItems: empty byDay');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["MO"],"bySetPos":[1,2]}', FALSE, '70 maxItems: two bySetPos');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","byDay":["MO","MO"]}', FALSE, '70 uniqueItems: byDay MO twice');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[5,5]}', FALSE, '70 uniqueItems: byMonthDay 5 twice');

-- dependencies and not: bySetPos only qualifies a byDay, and a series
-- ends by date or by count, never both.
CALL nf_conformance_expect_recurrence('{"freq":"monthly","bySetPos":[1]}', FALSE, '70 dependencies: bySetPos without byDay');
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":"2026-03-04","count":3}', FALSE, '70 not: until with count');

-- minLength and maxLength: until is a date or an instant, so shorter
-- than a date or longer than any instant is neither.
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":""}', FALSE, '70 minLength: empty until');
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":"2026-03-4"}', FALSE, '70 minLength: 9-character until');
CALL nf_conformance_expect_recurrence(
  CONCAT('{"freq":"daily","until":"', REPEAT('9', 41), '"}'), FALSE,
  '70 maxLength: 41-character until');

-- Accepted: the edges of each range, and what the CHECK leaves to the
-- writers. Which keys suit which freq, and whether until parses, are
-- each application's validation; refusing them here would put two
-- definitions of the rule in play.
CALL nf_conformance_expect_recurrence(NULL, TRUE, '70 accepted: SQL NULL, a single event');
CALL nf_conformance_expect_recurrence('{"freq":"daily"}', TRUE, '70 accepted: interval absent');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":1}', TRUE, '70 accepted: interval 1');
CALL nf_conformance_expect_recurrence('{"freq":"daily","interval":999}', TRUE, '70 accepted: interval 999');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":1}', TRUE, '70 accepted: count 1');
CALL nf_conformance_expect_recurrence('{"freq":"daily","count":1000}', TRUE, '70 accepted: count 1000');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[31]}', TRUE, '70 accepted: byMonthDay 31');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byMonthDay":[1,31]}', TRUE, '70 accepted: byMonthDay 1 and 31');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","byMonthDay":[3]}', TRUE, '70 accepted: byMonthDay on weekly');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["TH"],"bySetPos":[5]}', TRUE, '70 accepted: bySetPos 5');
CALL nf_conformance_expect_recurrence('{"freq":"monthly","byDay":["FR"],"bySetPos":[-1]}', TRUE, '70 accepted: bySetPos -1');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","byDay":["MO","TU","WE","TH","FR","SA","SU"]}', TRUE, '70 accepted: all seven days');
CALL nf_conformance_expect_recurrence('{"freq":"weekly","interval":2,"byDay":["TU","SU"],"wkst":"SU"}', TRUE, '70 accepted: wkst SU');
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":"2026-03-04"}', TRUE, '70 accepted: until as a date');
CALL nf_conformance_expect_recurrence('{"freq":"daily","until":"２０２６-03-04"}', TRUE, '70 accepted: non-ASCII until of 10 characters');
