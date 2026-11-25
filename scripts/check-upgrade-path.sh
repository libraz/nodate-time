#!/usr/bin/env bash
# check-upgrade-path.sh — prove an existing database upgrades in place.
#
#   bash scripts/check-upgrade-path.sh
#
# For each starting state (the v0.1.0 schema, the schema main carried before
# the core upgrades existed, and 7e9a601a, a database already past
# from-v0.1.0.sql), on a scratch database:
#   1. load that schema, plus scripts/fixtures/v0.1.0-data.sql for the states
#      that run from-v0.1.0.sql
#   2. apply sql/core/upgrades/*.sql in number order, then the time-layer
#      upgrades in operator order -- twice, to prove a re-run is harmless.
#      Just before 0004 the first pass loads scripts/fixtures/pre-0004-data.sql
#      (all-day rows in the old shapes and the operator's decisions) and runs
#      classify-all-day.sql over them
#   3. check the rewritten rows (fixture states) and the values 0004 gives
#      the pre-0004 rows (every state), run the conformance suite
#      (--mode all), and compare the schema against a fresh load of the
#      working tree's sql/schema.sql
# Every scratch database is dropped on exit, pass or fail.
#
# Environment:
#   TC_UPGRADE_CHECK_DSN        user:pass@host:port of a MySQL server the
#                               user may create databases on
#                               (default root:rootpw@127.0.0.1:33306)
#   TC_UPGRADE_CHECK_DB_PREFIX  scratch database name prefix (default sdb_up_)
#   TC_CORE_UPGRADES_DIR        core upgrades to apply (default sql/core/upgrades)
#
# Needs the mysql client and git (the starting schemas are read from history).
#
# Exit codes: 0 every state upgraded cleanly, 1 a check failed, 2 the run
# could not be performed.

set -euo pipefail
export LC_ALL=C

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="${ROOT}/scripts/fixtures/v0.1.0-data.sql"
PRE_0004_FIXTURE="${ROOT}/scripts/fixtures/pre-0004-data.sql"
CLASSIFY="${ROOT}/sql/core/tools/classify-all-day.sql"
RELEASE_FILE="${ROOT}/sql/time/upgrades/from-v0.1.0.sql"
# Feature files in operator order, then the generated triggers last.
FEATURE_FILES=(
  "${ROOT}/sql/time/upgrades/schedule-polls.sql"
  "${ROOT}/sql/time/upgrades/calendar-covers.sql"
  "${ROOT}/sql/time/upgrades/caldav.sql"
  "${ROOT}/sql/time/upgrades/event-labels.sql"
  "${ROOT}/sql/time/upgrades/triggers.sql"
)
CORE_UPGRADES="${TC_CORE_UPGRADES_DIR:-${ROOT}/sql/core/upgrades}"
DSN="${TC_UPGRADE_CHECK_DSN:-root:rootpw@127.0.0.1:33306}"
PREFIX="${TC_UPGRADE_CHECK_DB_PREFIX:-sdb_up_}"

# label:git-ref:release of every starting state; release is "run" when the
# state applies from-v0.1.0.sql and "skip" when it already has.
STATES=(
  "v010:v0.1.0:run"
  "head:08d32dc:run"
  "past:7e9a601a:skip"
)

if [[ ! "${DSN}" =~ ^([^:]+):([^@]*)@([^:/]+):([0-9]+)$ ]]; then
  echo "check-upgrade-path.sh: TC_UPGRADE_CHECK_DSN must be user:pass@host:port" >&2
  exit 2
fi
DB_USER="${BASH_REMATCH[1]}"
DB_PASS="${BASH_REMATCH[2]}"
DB_HOST="${BASH_REMATCH[3]}"
DB_PORT="${BASH_REMATCH[4]}"

if [[ ! "${PREFIX}" =~ ^[a-z0-9_]+$ ]]; then
  echo "check-upgrade-path.sh: TC_UPGRADE_CHECK_DB_PREFIX takes [a-z0-9_] only" >&2
  exit 2
fi
for cmd in mysql git; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "check-upgrade-path.sh: ${cmd} is required" >&2
    exit 2
  fi
done
shopt -s nullglob
CORE_FILES=("${CORE_UPGRADES}"/[0-9][0-9][0-9][0-9]-*.sql)
shopt -u nullglob
if [[ ${#CORE_FILES[@]} -eq 0 ]]; then
  echo "check-upgrade-path.sh: no core upgrades under ${CORE_UPGRADES}" >&2
  exit 2
fi
for f in "${FIXTURE}" "${PRE_0004_FIXTURE}" "${CLASSIFY}" "${RELEASE_FILE}" "${FEATURE_FILES[@]}"; do
  if [[ ! -f "${f}" ]]; then
    echo "check-upgrade-path.sh: missing ${f}" >&2
    exit 2
  fi
done

# sql [DB]: run stdin as one session; stops at the first error.
sql() {
  MYSQL_PWD="${DB_PASS}" mysql --host="${DB_HOST}" --port="${DB_PORT}" --user="${DB_USER}" \
    --protocol=TCP --default-character-set=utf8mb4 --batch --skip-column-names "$@"
}

dsn_of() { printf '%s:%s@%s:%s/%s' "${DB_USER}" "${DB_PASS}" "${DB_HOST}" "${DB_PORT}" "$1"; }

CREATED=()
cleanup() {
  local db
  for db in ${CREATED[@]+"${CREATED[@]}"}; do
    echo "DROP DATABASE IF EXISTS \`${db}\`;" | sql >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

create_db() {
  echo "DROP DATABASE IF EXISTS \`$1\`; CREATE DATABASE \`$1\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" | sql
  CREATED+=("$1")
}

# apply_upgrades DB RELEASE PASS: the order an operator follows, core first.
# On the first pass the pre-0004 rows and decisions go in just before 0004,
# and the classification report runs over them, as it does before 0004 in
# production.
apply_upgrades() {
  local f files=("${CORE_FILES[@]}")
  if [[ "$2" == run ]]; then
    files+=("${RELEASE_FILE}")
  fi
  files+=("${FEATURE_FILES[@]}")
  for f in "${files[@]}"; do
    if [[ "$3" == 1 && "$(basename "${f}")" == 0004-* ]]; then
      sql "$1" <"${PRE_0004_FIXTURE}" >/dev/null || { echo "  failed: $(basename "${PRE_0004_FIXTURE}")" >&2; return 1; }
      { echo "SET @nf_http_cutoff = '2026-01-01 00:00:00';"; cat "${CLASSIFY}"; } | sql "$1" >/dev/null \
        || { echo "  failed: $(basename "${CLASSIFY}")" >&2; return 1; }
    fi
    sql "$1" <"${f}" >/dev/null || { echo "  failed: $(basename "${f}")" >&2; return 1; }
  done
}

# Each query counts the fixture rows a rewrite missed; every count must be 0.
# The conformance suite covers kinds, ref_count, audience and e-mail case.
ROW_CHECKS=$(cat <<'SQL'
SELECT 'calendar.event.* payload still carrying id or summary, or lacking eventId/title', COUNT(*) FROM events
 WHERE type LIKE 'calendar.event.%'
   AND (JSON_CONTAINS_PATH(payload_json, 'one', '$.id', '$.summary')
        OR NOT JSON_CONTAINS_PATH(payload_json, 'all', '$.eventId', '$.title'));
SELECT 'calendar.event.comment.* payload lacking commentId', COUNT(*) FROM events
 WHERE type LIKE 'calendar.event.comment.%' AND NOT JSON_CONTAINS_PATH(payload_json, 'one', '$.commentId');
SELECT 'subject_public_id not the event on calendar.event.*', COUNT(*) FROM events e
 WHERE e.type LIKE 'calendar.event.%'
   AND NOT EXISTS (SELECT 1 FROM calendar_events ce WHERE ce.public_id = e.subject_public_id);
SELECT 'subject_public_id set on a kind without a subject', COUNT(*) FROM events
 WHERE subject_public_id IS NOT NULL
   AND (type IN ('calendar.created', 'calendar.updated', 'calendar.deleted', 'calendar.reminder')
        OR type LIKE 'calendar.member.%' OR type LIKE 'workspace.%');
SELECT 'memo/photo/invite payload still carrying id', COUNT(*) FROM events
 WHERE (type LIKE 'calendar.memo.%' OR type LIKE 'calendar.photo.%' OR type LIKE 'calendar.invite.%')
   AND JSON_CONTAINS_PATH(payload_json, 'one', '$.id');
SELECT 'attendee change not split into rsvp / attendee kinds', COUNT(*) FROM events
 WHERE (type = 'calendar.event.rsvp.updated' AND NOT JSON_CONTAINS_PATH(payload_json, 'one', '$.rsvp'))
    OR (type = 'calendar.event.attendee.updated' AND NOT JSON_CONTAINS_PATH(payload_json, 'one', '$.canEdit'));
SELECT 'weekly rule without wkst', COUNT(*) FROM calendar_events
 WHERE recurrence_rule->>'$.freq' = 'weekly' AND NOT JSON_CONTAINS_PATH(recurrence_rule, 'one', '$.wkst');
SELECT 'scalar byMonthDay / bySetPos left', COUNT(*) FROM calendar_events
 WHERE JSON_TYPE(recurrence_rule->'$.byMonthDay') NOT IN ('ARRAY')
    OR JSON_TYPE(recurrence_rule->'$.bySetPos') NOT IN ('ARRAY');
SELECT 'allowed e-mail rows not carried into oauth_signin_allowlist', 3 - COUNT(*) FROM oauth_signin_allowlist
 WHERE entry_kind = 'email'
   AND entry_value IN ('ops@example.com', 'guest@partner.example', 'former@partner.example');
SELECT 'allowlist entry not lower-case or losing its reason', COUNT(*) FROM oauth_signin_allowlist
 WHERE CAST(entry_value AS BINARY) <> CAST(LOWER(entry_value) AS BINARY)
    OR (entry_value = 'ops@example.com' AND (notes IS NULL OR notes = '' OR added_by_user_id IS NULL))
    OR (entry_value = 'former@partner.example' AND enabled);
SELECT 'thumbnail key still under the photo', COUNT(*) FROM storage_objects
 WHERE storage_key LIKE 'albums/%/thumb';
SELECT 'deleted attachment or photo not marked retired', COUNT(*) FROM (
  SELECT id FROM calendar_event_attachments
   WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at
  UNION ALL
  SELECT id FROM album_photos
   WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at) r;
SELECT 'session not stamped with the time audience', COUNT(*) FROM sessions WHERE audience <> 'time';
SELECT 'calendar member without a workspace row not given exactly one live member row', COUNT(*) FROM users u
 WHERE u.email = 'invited@example.com'
   AND 1 <> (SELECT COUNT(*) FROM workspace_members wm
             WHERE wm.user_id = u.id AND wm.workspace_id = 1 AND wm.enabled AND wm.role = 'member'
               AND SUBSTR(HEX(wm.public_id), 13, 1) = '7');
SELECT 'calendar member removed from the workspace still live, or revived', COUNT(*) FROM users u
 WHERE u.email = 'removed@example.com'
   AND (EXISTS (SELECT 1 FROM calendar_members cm WHERE cm.user_id = u.id AND cm.enabled)
        OR EXISTS (SELECT 1 FROM workspace_members wm WHERE wm.user_id = u.id AND wm.enabled));
SQL
)

# check_rows DB: print every rewrite check that found a leftover.
check_rows() {
  local out
  out="$(sql "$1" <<<"${ROW_CHECKS}")" || return 1
  if awk -F'\t' '$2 != 0 { print "  " $1 ": " $2; bad = 1 } END { exit bad }' <<<"${out}"; then
    return 0
  fi
  return 1
}

# Each query counts the pre-0004 fixture rows 0004 left with the wrong value;
# every count must be 0. The expectation tables hold one row per fixture row.
ALL_DAY_CHECKS=$(cat <<'SQL'
SET time_zone = '+00:00';
SELECT 'pre-0004 events missing', (SELECT COUNT(*) FROM fx_0004_expect_event) - COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id;
SELECT 'event start_at / end_at not converted', COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id
 WHERE NOT (ce.start_at <=> x.start_at AND ce.end_at <=> x.end_at);
SELECT 'series until not converted', COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id
 WHERE NOT (ce.recurrence_rule->>'$.until' <=> x.until_v);
SELECT 'recurrence_end not widened', COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id
 WHERE NOT (ce.recurrence_end <=> x.recurrence_end);
SELECT 'recurrence_exceptions not converted', COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id
 WHERE NOT (ce.recurrence_exceptions <=> x.exceptions);
SELECT 'override recurrence_original_start not converted', COUNT(*)
  FROM calendar_events ce JOIN fx_0004_expect_event x ON x.id = ce.id
 WHERE NOT (ce.recurrence_original_start <=> x.original_start);
SELECT 'converted event lost its updated_at', COUNT(*)
  FROM calendar_events WHERE id BETWEEN 9100 AND 9199 AND updated_at <> '2020-01-01 00:00:00';
SELECT 'sent-reminder occurrence_start not converted', COUNT(*)
  FROM calendar_event_reminders r JOIN fx_0004_expect_reminder x ON x.id = r.id
 WHERE r.occurrence_start <> x.occurrence_start;
SELECT 'sent-reminder rows missing', (SELECT COUNT(*) FROM fx_0004_expect_reminder) - COUNT(*)
  FROM calendar_event_reminders r JOIN fx_0004_expect_reminder x ON x.id = r.id;
SELECT 'log calendar_id not filled from the event', COUNT(*)
  FROM events e JOIN fx_0004_expect_log x ON x.id = e.id
 WHERE NOT (e.calendar_id <=> x.calendar_id);
SELECT 'all-day or notification CHECK missing', 2 - COUNT(*)
  FROM information_schema.TABLE_CONSTRAINTS
 WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events' AND CONSTRAINT_TYPE = 'CHECK'
   AND CONSTRAINT_NAME IN ('chk_calendar_events_all_day_canonical', 'chk_calendar_events_notification_offset_range');
SELECT 'conversion helper table left behind', COUNT(*)
  FROM information_schema.TABLES
 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME LIKE 'core\_upgrade\_0004\_%';
SQL
)

# check_all_day DB: print every all-day expectation 0004 missed, then drop
# the expectation tables so the schema comparison sees only the schema.
check_all_day() {
  local out rc=0
  out="$(sql "$1" <<<"${ALL_DAY_CHECKS}")" || return 1
  awk -F'\t' '$2 != 0 { print "  " $1 ": " $2; bad = 1 } END { exit bad }' <<<"${out}" || rc=1
  sql "$1" <<<"DROP TABLE IF EXISTS fx_0004_expect_event, fx_0004_expect_reminder, fx_0004_expect_log;" || return 1
  return "${rc}"
}

failed=0
fresh="${PREFIX}fresh"

echo "upgrade target: working tree sql/schema.sql -> ${fresh}"
create_db "${fresh}"
sql "${fresh}" <"${ROOT}/sql/schema.sql"

for state in "${STATES[@]}"; do
  IFS=: read -r label ref release <<<"${state}"
  db="${PREFIX}${label}"
  echo "== ${ref} -> ${db}"
  create_db "${db}"
  if ! git -C "${ROOT}" show "${ref}:sql/schema.sql" | sql "${db}"; then
    echo "  could not load ${ref}:sql/schema.sql" >&2
    exit 2
  fi
  # The fixture holds v0.1.0 rows that only from-v0.1.0.sql rewrites.
  if [[ "${release}" == run ]]; then
    sql "${db}" <"${FIXTURE}"
  fi

  ok=1
  for pass in 1 2; do
    echo "  upgrade pass ${pass}"
    apply_upgrades "${db}" "${release}" "${pass}" || { ok=0; break; }
  done
  if [[ ${ok} -eq 1 && "${release}" == run ]]; then
    echo "  rewritten rows"
    check_rows "${db}" || ok=0
  fi
  if [[ ${ok} -eq 1 ]]; then
    echo "  all-day rows"
    check_all_day "${db}" || ok=0
  fi
  if [[ ${ok} -eq 1 ]]; then
    echo "  conformance"
    bash "${ROOT}/sql/core/conformance/run.sh" --dsn "$(dsn_of "${db}")" --mode all || ok=0
    echo "  equivalence with ${fresh}"
    bash "${ROOT}/sql/core/tools/check-layer-equivalence.sh" \
      --dsn-a "$(dsn_of "${fresh}")" --dsn-b "$(dsn_of "${db}")" || ok=0
  fi
  if [[ ${ok} -eq 1 ]]; then
    echo "  ${ref}: upgraded cleanly"
  else
    echo "  ${ref}: FAILED" >&2
    failed=1
  fi
done

exit "${failed}"
