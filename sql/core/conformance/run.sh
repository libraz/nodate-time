#!/usr/bin/env bash
# run.sh — check a database against the nodate core contract.
#
#   bash sql/core/conformance/run.sh --dsn 'user:pass@host:port/dbname'
#   bash sql/core/conformance/run.sh --dsn ... --mode data
#
# Modes:
#   schema  (default) create fixtures, assert structure and guard
#           behaviour, remove the fixtures. Writes, so point it at a test
#           database.
#   data    sweep an existing database for rows no conforming writer could
#           have produced. Read-only; safe against production.
#   all     schema, then data.
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

  run.sh --dsn 'user:pass@host:port/dbname' [--mode schema|data|all]

The database may also be given as NF_CONFORMANCE_DSN.

Modes:
  schema  (default) create fixtures, assert structure and guard behaviour,
          remove the fixtures. Writes, so point it at a test database.
  data    sweep an existing database for rows no conforming writer could
          have produced. Read-only; safe against production.
  all     schema, then data.

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
  schema|data|all) ;;
  *) echo "--mode must be one of schema, data, all" >&2; exit 2 ;;
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
    --batch --silent "${DB_NAME}"
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
  if concat "${SCRIPT_DIR}/harness/00-procedures.sql" \
            "${SCRIPT_DIR}"/data/*.sql | run_sql; then
    echo "conformance: data mode passed"
  else
    echo "conformance: data mode FAILED" >&2
    failed=1
  fi
  # The procedures are the only thing data mode creates.
  printf 'DROP PROCEDURE IF EXISTS nf_conformance_assert;\nDROP PROCEDURE IF EXISTS nf_conformance_expect_rejected;\n' \
    | run_sql >/dev/null 2>&1 || true
fi

exit "${failed}"
