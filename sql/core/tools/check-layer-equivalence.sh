#!/usr/bin/env bash
# check-layer-equivalence.sh — compare the schema of two databases.
#
#   bash sql/core/tools/check-layer-equivalence.sh \
#        --dsn-a 'user:pass@host:port/db_a' --dsn-b 'user:pass@host:port/db_b' \
#        [--tables t1,t2,...]
#
# Needs the mysql client; the password is handed to it through MYSQL_PWD for
# that child process only. No other environment variable is read.
#
# Compared (from information_schema): table names; per column name, type,
# nullability, default, character set, collation, EXTRA, generation
# expression; per index name, uniqueness, type, ordered columns; per
# constraint name, kind, CHECK expression, FK target/columns/rules; view
# definitions; trigger name, table, timing, event, body.
# Not compared: column order, comments, AUTO_INCREMENT, table options.
#
# --tables limits tables, columns, indexes, constraints and triggers to the
# listed tables and skips views.
#
# Exit codes: 0 identical, 1 differences (printed as a diff), 2 bad usage or
# a database could not be read.

set -euo pipefail
export LC_ALL=C

usage() { sed -n '2,22p' "${BASH_SOURCE[0]}"; }

DSN_A=""
DSN_B=""
TABLES=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dsn-a) DSN_A="${2:-}"; shift 2 ;;
    --dsn-b) DSN_B="${2:-}"; shift 2 ;;
    --tables) TABLES="${2:-}"; shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) echo "check-layer-equivalence.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
if [[ -z "${DSN_A}" || -z "${DSN_B}" ]]; then
  usage >&2
  exit 2
fi
if [[ -n "${TABLES}" && "${TABLES}" == *[!A-Za-z0-9_,]* ]]; then
  echo "check-layer-equivalence.sh: --tables takes a comma-separated list of table names" >&2
  exit 2
fi
if ! command -v mysql >/dev/null 2>&1; then
  echo "check-layer-equivalence.sh: the mysql client is required" >&2
  exit 2
fi

DSN_RE='^([^:]+):([^@]*)@([^:/]+):([0-9]+)/(.+)$'

# tf COLUMN: restrict a query to --tables.
tf() {
  if [[ -n "${TABLES}" ]]; then
    printf "AND FIND_IN_SET(%s, '%s')" "$1" "${TABLES}"
  fi
}

dump_sql() {
  cat <<SQL
SET SESSION group_concat_max_len = 1048576;

SELECT 'TABLE', t.TABLE_NAME
FROM information_schema.TABLES t
WHERE t.TABLE_SCHEMA = DATABASE() AND t.TABLE_TYPE = 'BASE TABLE' $(tf t.TABLE_NAME);

SELECT 'COLUMN', c.TABLE_NAME, c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE,
       c.COLUMN_DEFAULT, c.CHARACTER_SET_NAME, c.COLLATION_NAME, c.EXTRA,
       c.GENERATION_EXPRESSION
FROM information_schema.COLUMNS c
JOIN information_schema.TABLES t
  ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
 AND t.TABLE_TYPE = 'BASE TABLE'
WHERE c.TABLE_SCHEMA = DATABASE() $(tf c.TABLE_NAME);

SELECT 'INDEX', s.TABLE_NAME, s.INDEX_NAME,
       IF(s.NON_UNIQUE = 0, 'unique', 'nonunique'), s.INDEX_TYPE,
       GROUP_CONCAT(
         CONCAT(IFNULL(s.COLUMN_NAME, CONCAT('(', s.EXPRESSION, ')')),
                IFNULL(CONCAT('/', s.SUB_PART), ''))
         ORDER BY s.SEQ_IN_INDEX SEPARATOR ',')
FROM information_schema.STATISTICS s
WHERE s.TABLE_SCHEMA = DATABASE() $(tf s.TABLE_NAME)
GROUP BY s.TABLE_NAME, s.INDEX_NAME, s.NON_UNIQUE, s.INDEX_TYPE;

SELECT 'CONSTRAINT', tc.TABLE_NAME, tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE,
  CASE tc.CONSTRAINT_TYPE
    WHEN 'CHECK' THEN (
      SELECT cc.CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS cc
      WHERE cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
        AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME)
    WHEN 'FOREIGN KEY' THEN (
      SELECT CONCAT(rc.REFERENCED_TABLE_NAME,
                    ' ON DELETE ', rc.DELETE_RULE, ' ON UPDATE ', rc.UPDATE_RULE,
                    ' (', (SELECT GROUP_CONCAT(
                                    CONCAT(k.COLUMN_NAME, '>', k.REFERENCED_COLUMN_NAME)
                                    ORDER BY k.ORDINAL_POSITION)
                           FROM information_schema.KEY_COLUMN_USAGE k
                           WHERE k.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
                             AND k.TABLE_NAME = rc.TABLE_NAME
                             AND k.CONSTRAINT_NAME = rc.CONSTRAINT_NAME), ')')
      FROM information_schema.REFERENTIAL_CONSTRAINTS rc
      WHERE rc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
        AND rc.TABLE_NAME = tc.TABLE_NAME
        AND rc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME)
    ELSE ''
  END
FROM information_schema.TABLE_CONSTRAINTS tc
WHERE tc.TABLE_SCHEMA = DATABASE() $(tf tc.TABLE_NAME);

SELECT 'TRIGGER', g.TRIGGER_NAME, g.EVENT_OBJECT_TABLE, g.ACTION_TIMING,
       g.EVENT_MANIPULATION, REPLACE(g.ACTION_STATEMENT, CONCAT('\`', DATABASE(), '\`.'), '\`<db>\`.')
FROM information_schema.TRIGGERS g
WHERE g.TRIGGER_SCHEMA = DATABASE() $(tf g.EVENT_OBJECT_TABLE);
SQL
  if [[ -z "${TABLES}" ]]; then
    cat <<'SQL'

SELECT 'VIEW', v.TABLE_NAME,
       REPLACE(v.VIEW_DEFINITION, CONCAT('`', DATABASE(), '`.'), '`<db>`.')
FROM information_schema.VIEWS v
WHERE v.TABLE_SCHEMA = DATABASE();
SQL
  fi
}

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

# dump DSN OUTFILE: normalised, sorted attribute lines for one database.
dump() {
  local dsn="$1" out="$2"
  if [[ ! "${dsn}" =~ ${DSN_RE} ]]; then
    echo "check-layer-equivalence.sh: could not parse DSN; expected user:pass@host:port/dbname" >&2
    exit 2
  fi
  if ! dump_sql | MYSQL_PWD="${BASH_REMATCH[2]}" mysql \
    --host="${BASH_REMATCH[3]}" --port="${BASH_REMATCH[4]}" --user="${BASH_REMATCH[1]}" \
    --protocol=TCP --default-character-set=utf8mb4 \
    --batch --skip-column-names "${BASH_REMATCH[5]}" >"${out}.raw"; then
    echo "check-layer-equivalence.sh: could not read ${BASH_REMATCH[3]}:${BASH_REMATCH[4]}/${BASH_REMATCH[5]}" >&2
    exit 2
  fi
  sort "${out}.raw" >"${out}"
}

dump "${DSN_A}" "${TMP}/a"
dump "${DSN_B}" "${TMP}/b"

if diff -u -L A -L B "${TMP}/a" "${TMP}/b" >"${TMP}/diff"; then
  echo "schemas are equivalent ($(wc -l <"${TMP}/a" | tr -d ' ') attributes compared)"
  exit 0
fi
echo "schemas differ (- only in A, + only in B):"
cat "${TMP}/diff"
exit 1
