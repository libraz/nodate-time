#!/usr/bin/env bash
# run.sh — check a database against the nodate core contract.
#
#   bash sql/core/conformance/run.sh --dsn 'user:pass@host:port/dbname'
#   bash sql/core/conformance/run.sh --dsn ... --mode data
#
# Modes:
#   schema    (default) create fixtures, assert structure and guard
#             behaviour, remove the fixtures. Writes, so point it at a test
#             database.
#   data      sweep an existing database for rows no conforming writer
#             could have produced. Read-only; safe against production.
#   all       schema, then data.
#   selftest  prove the checks are not vacuous: every recurrence fixture is
#             stored by the shape CHECK, and each case under
#             selftest/cases/ plants one violation inside a transaction
#             that is never committed and must make its check fail. Writes,
#             so point it at a test database.
#
# Exit codes:
#   0  the database conforms
#   1  a check failed
#   2  the run could not be performed (bad arguments, no mysql client)
#
# The suite is plain SQL driven by this script so that implementing the
# contract does not commit a project to any particular language. Each file
# is concatenated into a single mysql session, because the checks pass
# fixture ids to each other through session variables.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

DSN=""
MODE="schema"

usage() {
  cat <<'USAGE'
run.sh — check a database against the nodate core contract.

  run.sh --dsn 'user:pass@host:port/dbname' [--mode schema|data|all|selftest]

The database may also be given as NF_CONFORMANCE_DSN.

Modes:
  schema    (default) create fixtures, assert structure and guard behaviour,
            remove the fixtures. Writes, so point it at a test database.
  data      sweep an existing database for rows no conforming writer could
            have produced. Read-only; safe against production.
  all       schema, then data.
  selftest  plant one violation per check and confirm each is caught.
            Writes, so point it at a test database.

Exit codes:
  0  the database conforms
  1  a check failed
  2  the run could not be performed
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dsn)  DSN="${2:-}"; shift 2 ;;
    --mode) MODE="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "${MODE}" in
  schema|data|all|selftest) ;;
  *) echo "--mode must be one of schema, data, all, selftest" >&2; exit 2 ;;
esac

if [[ -z "${DSN}" ]]; then
  DSN="${NF_CONFORMANCE_DSN:-}"
fi
if [[ -z "${DSN}" ]]; then
  echo "no database given: pass --dsn or set NF_CONFORMANCE_DSN" >&2
  exit 2
fi

if ! command -v mysql >/dev/null 2>&1; then
  echo "the mysql client is required to run the conformance suite" >&2
  exit 2
fi

# Parse user:pass@host:port/dbname. Kept deliberately simple — this is the
# form both products already use for their own tooling, and anything more
# permissive would silently accept a DSN it then mis-parses.
if [[ ! "${DSN}" =~ ^([^:]+):([^@]*)@([^:/]+):([0-9]+)/(.+)$ ]]; then
  echo "could not parse DSN; expected user:pass@host:port/dbname" >&2
  exit 2
fi
DB_USER="${BASH_REMATCH[1]}"
DB_PASS="${BASH_REMATCH[2]}"
DB_HOST="${BASH_REMATCH[3]}"
DB_PORT="${BASH_REMATCH[4]}"
DB_NAME="${BASH_REMATCH[5]}"

run_sql() {
  # --batch keeps output parseable and -v off; the client exits non-zero on
  # the first error, which is how an assertion failure ends the run.
  MYSQL_PWD="${DB_PASS}" mysql \
    --host="${DB_HOST}" --port="${DB_PORT}" --user="${DB_USER}" \
    --default-character-set=utf8mb4 --batch --silent "${DB_NAME}"
}

# concat prints the given files in order, so the whole check runs in one
# session and session variables survive between files.
concat() {
  local f
  for f in "$@"; do
    [[ -e "${f}" ]] || continue
    printf -- '-- >>> %s\n' "${f#"${SCRIPT_DIR}/"}"
    cat "${f}"
    printf '\n'
  done
}

teardown() {
  concat "${SCRIPT_DIR}/harness/00-procedures.sql" \
         "${SCRIPT_DIR}/harness/99-teardown.sql" | run_sql >/dev/null 2>&1 || true
}

# drop_procedures removes what harness/00-procedures.sql creates, reading
# the names from its own DROP lines so the two cannot drift.
drop_procedures() {
  grep -E '^DROP PROCEDURE IF EXISTS ' "${SCRIPT_DIR}/harness/00-procedures.sql" \
    | run_sql >/dev/null 2>&1 || true
}

# Every event-kind registry deployed beside this suite: core's own and one
# per product layer, as sql/<layer>/event-kinds.yaml.
shopt -s nullglob
REGISTRIES=("${SCRIPT_DIR}"/../../*/event-kinds.yaml)
shopt -u nullglob

# registry_sql loads the declared kinds into nf_registered_kinds for the
# event-kind check. Only the `- kind:` lines are read, so no YAML parser is
# needed, and every value is held to the kind alphabet before it is
# spliced into SQL.
registry_sql() {
  local file kind
  printf 'DROP TEMPORARY TABLE IF EXISTS nf_registered_kinds;\n'
  printf 'CREATE TEMPORARY TABLE nf_registered_kinds (kind VARBINARY(64) NOT NULL PRIMARY KEY) ENGINE=InnoDB;\n'
  for file in "${REGISTRIES[@]}"; do
    awk '/^[[:space:]]*-[[:space:]]*kind:/ {
           v = $0
           sub(/^[^:]*:[[:space:]]*/, "", v)
           sub(/[[:space:]]*(#.*)?$/, "", v)
           gsub(/["\047]/, "", v)
           print v
         }' "${file}"
  done | sort -u | while IFS= read -r kind; do
    printf "INSERT INTO nf_registered_kinds (kind) VALUES ('%s');\n" "${kind}"
  done
}

check_registries() {
  local file bad
  if [[ ${#REGISTRIES[@]} -eq 0 ]]; then
    echo "no event-kinds.yaml found beside the suite; expected at least sql/core/event-kinds.yaml" >&2
    exit 2
  fi
  for file in "${REGISTRIES[@]}"; do
    bad="$(awk '/^[[:space:]]*-[[:space:]]*kind:/ {
                  v = $0
                  sub(/^[^:]*:[[:space:]]*/, "", v)
                  sub(/[[:space:]]*(#.*)?$/, "", v)
                  gsub(/["\047]/, "", v)
                  if (v !~ /^[a-z][a-z0-9_.]*$/) print FILENAME ": " v
                }' "${file}")"
    if [[ -n "${bad}" ]]; then
      echo "event-kind registry entries outside [a-z0-9_.]: ${bad}" >&2
      exit 2
    fi
  done
}

# fixtures_sql hands sql/core/recurrence/fixtures.json to the session as a
# hex literal, which needs no escaping whatever the file contains.
FIXTURES="${SCRIPT_DIR}/../recurrence/fixtures.json"
fixtures_sql() {
  printf "SET @nf_recurrence_fixtures = CONVERT(X'%s' USING utf8mb4);\n" \
    "$(od -An -v -tx1 "${FIXTURES}" | tr -d ' \n')"
}

failed=0

if [[ "${MODE}" == "schema" || "${MODE}" == "all" ]]; then
  echo "conformance: schema mode against ${DB_USER}@${DB_HOST}:${DB_PORT}/${DB_NAME}"
  # Clear anything a previous interrupted run left behind, so a stale
  # fixture cannot fail the run with a duplicate key.
  teardown
  if concat "${SCRIPT_DIR}/harness/00-procedures.sql" \
            "${SCRIPT_DIR}/harness/10-fixtures.sql" \
            "${SCRIPT_DIR}"/schema/*.sql \
            "${SCRIPT_DIR}/harness/99-teardown.sql" | run_sql; then
    echo "conformance: schema mode passed"
  else
    echo "conformance: schema mode FAILED" >&2
    failed=1
    teardown
  fi
fi

if [[ "${MODE}" == "data" || "${MODE}" == "all" ]]; then
  echo "conformance: data mode against ${DB_USER}@${DB_HOST}:${DB_PORT}/${DB_NAME}"
  check_registries
  if { concat "${SCRIPT_DIR}/harness/00-procedures.sql"
       registry_sql
       concat "${SCRIPT_DIR}"/data/*.sql; } | run_sql; then
    echo "conformance: data mode passed"
  else
    echo "conformance: data mode FAILED" >&2
    failed=1
  fi
  # The procedures are the only thing data mode creates outside the
  # session; its temporary tables leave with the connection.
  drop_procedures
fi

# planted_run prints one selftest session: the harness, then the fixtures,
# the optional case file and the check file inside a transaction that is
# rolled back, or dropped with the connection when the check fails.
planted_run() {
  local case_file="$1" check_file="$2"
  concat "${SCRIPT_DIR}/harness/00-procedures.sql"
  registry_sql
  printf 'START TRANSACTION;\n'
  concat "${SCRIPT_DIR}/harness/10-fixtures.sql"
  [[ -n "${case_file}" ]] && concat "${case_file}"
  [[ -n "${check_file}" ]] && concat "${check_file}"
  printf 'ROLLBACK;\n'
}

if [[ "${MODE}" == "selftest" ]]; then
  echo "conformance: selftest against ${DB_USER}@${DB_HOST}:${DB_PORT}/${DB_NAME}"
  check_registries
  if [[ ! -f "${FIXTURES}" ]]; then
    echo "recurrence fixtures not found at ${FIXTURES}" >&2
    exit 2
  fi
  teardown

  # The shape CHECK against the edge values, then every fixture rule.
  if { concat "${SCRIPT_DIR}/harness/00-procedures.sql" \
              "${SCRIPT_DIR}/harness/10-fixtures.sql" \
              "${SCRIPT_DIR}"/schema/70-*.sql
       fixtures_sql
       concat "${SCRIPT_DIR}/selftest/10-fixture-rules.sql" \
              "${SCRIPT_DIR}/harness/99-teardown.sql"; } | run_sql; then
    echo "conformance: selftest: recurrence shape and fixture rules passed"
  else
    echo "conformance: selftest: recurrence shape or fixture rules FAILED" >&2
    failed=1
    teardown
  fi

  # Each case names its check by its numeric prefix. Where a data check of
  # that number exists, it must pass without the case first, so the
  # failure that follows is the case's and not something already in the
  # database; the case must then fail it with that check's own message.
  baselined=" "
  cases=("${SCRIPT_DIR}"/selftest/cases/*.sql)
  for case_file in "${cases[@]}"; do
    [[ -e "${case_file}" ]] || continue
    name="$(basename "${case_file}" .sql)"
    num="${name%%-*}"
    check_file=""
    for f in "${SCRIPT_DIR}"/data/"${num}"-*.sql; do
      [[ -e "${f}" ]] && check_file="${f}"
    done

    if [[ -n "${check_file}" && "${baselined}" != *" ${num} "* ]]; then
      if ! out="$(planted_run "" "${check_file}" | run_sql 2>&1)"; then
        echo "conformance: selftest: check ${num} fails before any violation is planted:" >&2
        printf '%s\n' "${out}" >&2
        failed=1
      fi
      baselined+="${num} "
    fi

    if out="$(planted_run "${case_file}" "${check_file}" | run_sql 2>&1)"; then
      echo "conformance: selftest: ${name}: the planted violation was not caught" >&2
      failed=1
    elif [[ "${out}" != *"conformance failure: ${num} "* ]]; then
      echo "conformance: selftest: ${name}: failed, but not with check ${num}'s message:" >&2
      printf '%s\n' "${out}" >&2
      failed=1
    else
      echo "conformance: selftest: ${name}: caught"
    fi
  done

  teardown
  if [[ "${failed}" -eq 0 ]]; then
    echo "conformance: selftest passed"
  else
    echo "conformance: selftest FAILED" >&2
  fi
fi

exit "${failed}"
