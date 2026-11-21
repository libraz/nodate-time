#!/usr/bin/env bash
# test-0004.sh — prove upgrades/0004-all-day-canonical.sql on scratch databases.
#
#   bash sql/core/tools/test-0004.sh
#
# Every database starts from the schema as it was before 0004 (sql/schema.sql
# at NF_0004_BASE_REF) with fixtures/0004-base.sql planted, and first proves
# that 0004 stops on the two rows only an operator can settle, listing
# exactly those and changing nothing; fixtures/0004-decisions.sql then
# settles them. From there:
#   main     0004 converts every row to the value the fixture expects, adds
#            both CHECKs with the text and column comments a fresh load of
#            sql/core/tables/ has, drops its helpers; a second run changes
#            nothing
#   resume   0004 stopped by @core_0004_test_stop_after_convert right after
#            its commit restores the session and leaves the mark; the re-run
#            ends with the same rows as main
#   stop-*   each fixtures/0004-stop-*.sql plants rows that must stop the
#            run; it signals and lists exactly the `-- expect:` pairs
# Every scratch database is dropped on exit, pass or fail.
#
# Environment:
#   NF_0004_TEST_DSN        user:pass@host:port of a MySQL server the user may
#                           create databases on
#                           (default root:rootpw@127.0.0.1:33306)
#   NF_0004_TEST_DB_PREFIX  scratch database name prefix (default sdb_0004_)
#   NF_0004_BASE_REF        commit whose sql/schema.sql is the pre-0004 schema
#                           (default 07ee9584, nodate-flow before 0004)
#   NF_0004_UPGRADE         upgrade file under test (default upgrades/0004-*)
#
# Needs the mysql client and git.
#
# Exit codes: 0 every check passed, 1 a check failed, 2 the run could not be
# performed.

set -euo pipefail
export LC_ALL=C

CORE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO="$(cd "${CORE}/../.." && pwd)"
FIXTURES="${CORE}/tools/fixtures"
UPGRADE="${NF_0004_UPGRADE:-${CORE}/upgrades/0004-all-day-canonical.sql}"
BASE_REF="${NF_0004_BASE_REF:-07ee9584fe37e73c47e5dfac97c168e6e2984376}"
DSN="${NF_0004_TEST_DSN:-root:rootpw@127.0.0.1:33306}"
PREFIX="${NF_0004_TEST_DB_PREFIX:-sdb_0004_}"

if [[ ! "${DSN}" =~ ^([^:]+):([^@]*)@([^:/]+):([0-9]+)$ ]]; then
  echo "test-0004.sh: NF_0004_TEST_DSN must be user:pass@host:port" >&2
  exit 2
fi
DB_USER="${BASH_REMATCH[1]}"
DB_PASS="${BASH_REMATCH[2]}"
DB_HOST="${BASH_REMATCH[3]}"
DB_PORT="${BASH_REMATCH[4]}"

if [[ ! "${PREFIX}" =~ ^[a-z0-9_]+$ ]]; then
  echo "test-0004.sh: NF_0004_TEST_DB_PREFIX takes [a-z0-9_] only" >&2
  exit 2
fi
for cmd in mysql git; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "test-0004.sh: ${cmd} is required" >&2
    exit 2
  fi
done
for f in "${UPGRADE}" "${FIXTURES}/0004-base.sql" "${FIXTURES}/0004-decisions.sql"; do
  if [[ ! -f "${f}" ]]; then
    echo "test-0004.sh: missing ${f}" >&2
    exit 2
  fi
done
BASE_SCHEMA="$(git -C "${REPO}" show "${BASE_REF}:sql/schema.sql" 2>/dev/null)" || {
  echo "test-0004.sh: cannot read ${BASE_REF}:sql/schema.sql; set NF_0004_BASE_REF to a commit before 0004" >&2
  exit 2
}

# sql [ARGS]: run stdin as one session; stops at the first error.
sql() {
  MYSQL_PWD="${DB_PASS}" mysql --host="${DB_HOST}" --port="${DB_PORT}" --user="${DB_USER}" \
    --protocol=TCP --default-character-set=utf8mb4 --batch --skip-column-names "$@"
}

CREATED=()
cleanup() {
  local db
  for db in ${CREATED[@]+"${CREATED[@]}"}; do
    echo "DROP DATABASE IF EXISTS \`${db}\`;" | sql >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

failures=0
fail() {
  echo "  FAIL: $*" >&2
  failures=$((failures + 1))
}
pass() { echo "  ok: $*"; }

create_db() {
  echo "DROP DATABASE IF EXISTS \`$1\`; CREATE DATABASE \`$1\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" | sql
  CREATED+=("$1")
}

# run_upgrade DB: apply the upgrade; its output and errors land in RUN_LOG.
RUN_LOG=""
run_upgrade() {
  local rc=0
  RUN_LOG="$(sql "$1" <"${UPGRADE}" 2>&1)" || rc=$?
  return "${rc}"
}

# listed: the (reason, row id) pairs the last stop listed, sorted and unique.
listed() {
  awk -F'\t' '$2 ~ /^0190000000007000800000000000[0-9][0-9][0-9][0-9]$/ { print $1 " " substr($2, 29, 4) }' \
    <<<"${RUN_LOG}" | sort -u
}

# expect_stop DB LABEL EXPECTED: the run signals and lists exactly EXPECTED.
expect_stop() {
  if run_upgrade "$1"; then
    fail "$2: 0004 did not stop"
    return
  fi
  if ! grep -q 'ERROR 1644 (45000).*0004: ' <<<"${RUN_LOG}"; then
    fail "$2: stopped without the 0004 signal: ${RUN_LOG:0:300}"
    return
  fi
  local got
  got="$(listed)"
  if [[ "${got}" == "$3" ]]; then
    pass "$2: signalled and listed $(tr '\n' ' ' <<<"${got}")"
  else
    fail "$2: listed [$(tr '\n' ' ' <<<"${got}")], expected [$(tr '\n' ' ' <<<"$3")]"
  fi
}

SNAPSHOT=$(cat <<'SQL'
SET time_zone = '+00:00';
SELECT id, HEX(public_id), all_day, kind, start_at, end_at, timezone, recurrence_rule, recurrence_end,
       recurrence_exceptions, recurrence_parent_id, recurrence_original_start, notification_offset,
       task_id, enabled, updated_at
FROM calendar_events ORDER BY id;
SELECT id, event_id, occurrence_start FROM calendar_event_reminders ORDER BY id;
SELECT id, calendar_id, updated_at FROM events ORDER BY id;
SQL
)

snapshot() { sql "$1" <<<"${SNAPSHOT}"; }

# Each query prints one line per row that differs from the fixture's expectation.
EXPECTATIONS=$(cat <<'SQL'
SET time_zone = '+00:00';
SELECT CONCAT('event ', x.id, ': start ', COALESCE(ce.start_at, '-'), ' end ', COALESCE(ce.end_at, '-'),
              ' until ', COALESCE(JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')), '-'),
              ' recurrence_end ', COALESCE(ce.recurrence_end, '-'),
              ' exceptions ', COALESCE(CAST(ce.recurrence_exceptions AS CHAR), '-'),
              ' original_start ', COALESCE(ce.recurrence_original_start, '-'),
              ' updated_at ', COALESCE(ce.updated_at, '-'))
FROM fx_0004_expect_event x
LEFT JOIN calendar_events ce ON ce.id = x.id
WHERE NOT (ce.id IS NOT NULL
           AND ce.start_at <=> x.start_at
           AND ce.end_at <=> x.end_at
           AND JSON_UNQUOTE(JSON_EXTRACT(ce.recurrence_rule, '$.until')) <=> x.until_v
           AND ce.recurrence_end <=> x.recurrence_end
           AND CAST(ce.recurrence_exceptions AS CHAR) <=> CAST(x.exceptions AS CHAR)
           AND ce.recurrence_original_start <=> x.original_start
           AND ce.updated_at = '2020-01-01 00:00:00');
SELECT CONCAT('event ', ce.id, ': no expectation')
FROM calendar_events ce
WHERE NOT EXISTS (SELECT 1 FROM fx_0004_expect_event x WHERE x.id = ce.id);
SELECT CONCAT('reminder ', x.id, ': ', COALESCE(r.occurrence_start, '-'))
FROM fx_0004_expect_reminder x
LEFT JOIN calendar_event_reminders r ON r.id = x.id
WHERE NOT (r.id IS NOT NULL AND r.occurrence_start = x.occurrence_start);
SELECT CONCAT('log ', x.id, ': calendar_id ', COALESCE(e.calendar_id, '-'), ' updated_at ', COALESCE(e.updated_at, '-'))
FROM fx_0004_expect_log x
LEFT JOIN events e ON e.id = x.id
WHERE NOT (e.id IS NOT NULL AND e.calendar_id <=> x.calendar_id AND e.updated_at = '2020-01-01 00:00:00');
SQL
)

# What 0004 leaves behind, compared with a fresh load of sql/core/tables/.
SHAPE=$(cat <<'SQL'
SELECT CONSTRAINT_NAME, CHECK_CLAUSE
FROM information_schema.CHECK_CONSTRAINTS
WHERE CONSTRAINT_SCHEMA = DATABASE()
  AND CONSTRAINT_NAME IN ('chk_calendar_events_all_day_canonical', 'chk_calendar_events_notification_offset_range')
ORDER BY CONSTRAINT_NAME;
SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COALESCE(COLUMN_DEFAULT, '<null>'),
       COALESCE(CHARACTER_SET_NAME, '-'), COALESCE(COLLATION_NAME, '-'), COLUMN_COMMENT
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE()
  AND (TABLE_NAME, COLUMN_NAME) IN (('calendar_events', 'visibility'), ('calendar_events', 'start_at'),
                                    ('calendar_events', 'end_at'), ('calendar_events', 'recurrence_exceptions'),
                                    ('calendar_events', 'notification_offset'),
                                    ('calendars', 'default_event_visibility'), ('events', 'calendar_id'))
ORDER BY TABLE_NAME, COLUMN_NAME;
SQL
)

LEFTOVERS=$(cat <<'SQL'
SELECT CONCAT('table ', TABLE_NAME) FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME LIKE 'core\_upgrade\_0004\_%';
SELECT CONCAT('routine ', ROUTINE_NAME) FROM information_schema.ROUTINES
WHERE ROUTINE_SCHEMA = DATABASE() AND ROUTINE_NAME = 'core_upgrade_0004';
SQL
)

DDL='SHOW CREATE TABLE calendar_events; SHOW CREATE TABLE calendars; SHOW CREATE TABLE events'

# constraint_text FILE NAME: the CONSTRAINT clause as written, up to its
# balancing parenthesis, without the trailing comma or semicolon.
constraint_text() {
  awk -v name="$2" '
    index($0, "CONSTRAINT " name " CHECK") { on = 1 }
    on {
      line = $0
      depth += gsub(/\(/, "(", line) - gsub(/\)/, ")", line)
      if (depth == 0) { sub(/[,;]$/, "", line); print line; exit }
      print line
    }
  ' "$1"
}

# prepare DB LABEL: the pre-0004 schema with the base fixture; 0004 stops on
# the two undecided rows without touching anything; then decide them.
prepare() {
  create_db "$1"
  if ! sql "$1" <<<"${BASE_SCHEMA}"; then
    echo "test-0004.sh: could not load ${BASE_REF}:sql/schema.sql" >&2
    exit 2
  fi
  if ! sql "$1" <"${FIXTURES}/0004-base.sql"; then
    echo "test-0004.sh: could not plant fixtures/0004-base.sql" >&2
    exit 2
  fi
  local before err
  before="$(snapshot "$1")"
  expect_stop "$1" "$2 undecided rows" "$(printf 'unclassified 0112\nunclassified 0113')"
  if [[ "$(snapshot "$1")" != "${before}" ]]; then
    fail "$2: a stopped run changed rows"
  fi
  if ! err="$(sql "$1" <"${FIXTURES}/0004-decisions.sql" 2>&1)"; then
    fail "$2: could not fill the decision table: ${err:0:300}"
    return 1
  fi
}

echo "== fresh load of sql/core/tables/"
fresh="${PREFIX}fresh"
create_db "${fresh}"
bash "${CORE}/tools/build-schema.sh" core | sql "${fresh}"
FRESH_SHAPE="$(sql "${fresh}" <<<"${SHAPE}")"

echo "== constraint text"
for name in chk_calendar_events_all_day_canonical chk_calendar_events_notification_offset_range; do
  table_text="$(constraint_text "${CORE}/tables/calendar_events.sql" "${name}")"
  upgrade_text="$(constraint_text "${UPGRADE}" "${name}")"
  if [[ -n "${table_text}" && "${table_text}" == "${upgrade_text}" ]]; then
    pass "${name} byte-identical to tables/calendar_events.sql"
  else
    fail "${name} differs from tables/calendar_events.sql"
  fi
done

echo "== exception element grammar"
# grammar_check LABEL DATE_RE RFC_RE FLOAT_RE: each regex is a SQL string
# literal; every element of recurrence/exception-forms.json must read as its form.
grammar_check() {
  if [[ -z "$2" || -z "$3" || -z "$4" ]]; then
    fail "$1: element regexes not found"
    return
  fi
  local forms out
  forms="$(sed -e 's/\\/\\\\/g' -e "s/'/''/g" "${CORE}/recurrence/exception-forms.json")"
  if ! out="$(sql "${fresh}" 2>&1 <<SQL
SELECT CONCAT('"', jt.element, '" is ', jt.form, ', read as ', g.form)
FROM JSON_TABLE(CAST('${forms}' AS JSON), '\$.cases[*]'
       COLUMNS (element VARCHAR(255) PATH '\$.element', form VARCHAR(16) PATH '\$.form')) jt
CROSS JOIN LATERAL (
  SELECT CASE
           WHEN REGEXP_LIKE(jt.element, $2, 'c') THEN 'date'
           WHEN REGEXP_LIKE(jt.element, $3, 'c') THEN 'rfc3339'
           WHEN REGEXP_LIKE(jt.element, $4, 'c') THEN 'floating'
           ELSE 'unreadable'
         END AS form
) g
WHERE g.form <> jt.form;
SQL
)"; then
    fail "$1: grammar check could not run: ${out:0:300}"
  elif [[ -z "${out}" ]]; then
    pass "$1 reads every exception-forms.json element as its form"
  else
    fail "$1 disagrees with exception-forms.json:"$'\n'"${out}"
  fi
}
upgrade_re() { sed -n "s/^ *DECLARE v_re_$1 VARCHAR([0-9]*) DEFAULT \('.*'\);\$/\1/p" "${UPGRADE}"; }
classify_re() { sed -n "s/^SET @nf_classify_re_$1 = \('.*'\);\$/\1/p" "${CORE}/tools/classify-all-day.sql"; }
grammar_check "0004" "$(upgrade_re date)" "$(upgrade_re rfc)" "$(upgrade_re float)"
grammar_check "classify-all-day.sql" "$(classify_re date)" "$(classify_re rfc)" "$(classify_re float)"

echo "== main"
db="${PREFIX}main"
MAIN_ROWS=""
if prepare "${db}" main; then
  if run_upgrade "${db}"; then
    pass "0004 applied"
    out="$(sql "${db}" <<<"${EXPECTATIONS}")"
    if [[ -z "${out}" ]]; then
      pass "every row holds its expected value"
    else
      fail "rows differ from the fixture:"$'\n'"${out}"
    fi
    out="$(sql "${db}" <<<"${SHAPE}")"
    if [[ "${out}" == "${FRESH_SHAPE}" ]]; then
      pass "CHECKs and column comments match a fresh load"
    else
      fail "CHECKs or column comments differ from a fresh load:"$'\n'"$(diff <(echo "${FRESH_SHAPE}") <(echo "${out}") || true)"
    fi
    out="$(sql "${db}" <<<"${LEFTOVERS}")"
    if [[ -z "${out}" ]]; then
      pass "helper tables and procedure removed"
    else
      fail "left behind: ${out}"
    fi
    before="$(snapshot "${db}"; sql "${db}" -e "${DDL}")"
    if run_upgrade "${db}"; then
      if [[ "$(snapshot "${db}"; sql "${db}" -e "${DDL}")" == "${before}" ]]; then
        pass "second run changed nothing"
      else
        fail "second run changed the database"
      fi
    else
      fail "second run failed: ${RUN_LOG:0:300}"
    fi
    MAIN_ROWS="$(snapshot "${db}")"
  else
    fail "0004 failed: ${RUN_LOG:0:500}"
  fi
fi

echo "== resume"
db="${PREFIX}resume"
if prepare "${db}" resume; then
  # --force so the session survives the signal and can report its state.
  out="$({
    echo "SET SESSION time_zone = '+05:00';"
    echo "SET @nf_item_projection_engine = 7;"
    echo "SET @core_0004_test_stop_after_convert = 1;"
    echo "source ${UPGRADE}"
    echo "SELECT 'session', @@SESSION.time_zone, COALESCE(@nf_item_projection_engine, 'cleared');"
  } | sql --force "${db}" 2>&1)" || true
  if grep -q 'stopped after the conversion' <<<"${out}"; then
    pass "stopped after the conversion"
  else
    fail "the test hook did not stop the run: ${out:0:300}"
  fi
  if grep -q $'^session\t+05:00\tcleared$' <<<"${out}"; then
    pass "time_zone restored and the projection guard flag cleared"
  else
    fail "session not restored: $(grep '^session' <<<"${out}" || true)"
  fi
  state="$(sql "${db}" -e "SELECT
      (SELECT COUNT(*) FROM core_upgrade_0004_state WHERE step = 'converted'),
      (SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
        WHERE CONSTRAINT_SCHEMA = DATABASE() AND CONSTRAINT_NAME = 'chk_calendar_events_all_day_canonical')")"
  if [[ "${state}" == $'1\t0' ]]; then
    pass "converted mark committed, CHECK not yet added"
  else
    fail "unexpected state after the stop (converted, check): ${state}"
  fi
  if run_upgrade "${db}"; then
    out="$(snapshot "${db}")"
    if [[ -n "${MAIN_ROWS}" && "${out}" == "${MAIN_ROWS}" ]]; then
      pass "re-run ends with the same rows as a single run"
    else
      fail "re-run differs from a single run:"$'\n'"$(diff <(echo "${MAIN_ROWS}") <(echo "${out}") || true)"
    fi
  else
    fail "re-run failed: ${RUN_LOG:0:300}"
  fi
fi

shopt -s nullglob
STOPS=("${FIXTURES}"/0004-stop-*.sql)
shopt -u nullglob
for f in "${STOPS[@]}"; do
  label="$(basename "${f}" .sql)"
  label="${label#0004-}"
  echo "== ${label}"
  db="${PREFIX}${label//-/_}"
  prepare "${db}" "${label}" || continue
  if ! sql "${db}" <"${f}"; then
    echo "test-0004.sh: could not plant $(basename "${f}")" >&2
    exit 2
  fi
  expected="$(sed -n 's/^-- expect: //p' "${f}" | sort -u)"
  expect_stop "${db}" "${label}" "${expected}"
  out="$(sql "${db}" -e "SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND CONSTRAINT_NAME = 'chk_calendar_events_all_day_canonical'")"
  if [[ "${out}" != 0 ]]; then
    fail "${label}: the CHECK was added despite the stop"
  fi
done

if [[ ${failures} -gt 0 ]]; then
  echo "test-0004.sh: ${failures} check(s) failed" >&2
  exit 1
fi
echo "test-0004.sh: all checks passed"
