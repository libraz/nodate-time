#!/usr/bin/env bash
# build-schema.sh — print the schema of the given layers as one SQL stream.
#
#   bash sql/core/tools/build-schema.sh <layer>... | mysql ... <db>
#
# Examples:
#   build-schema.sh core            core layer only
#   build-schema.sh core time       a time-only deployment
#   build-schema.sh core time flow  everything
#   build-schema.sh flow            add the flow layer to a database that
#                                   already holds core
#
# Only the objects of the layers named on the command line are dropped and
# recreated, so a layer can be added to (or rebuilt on) a live database that
# holds the layers below it. Never run core or time against a database that
# already holds data; those are brought forward by their upgrades/ instead.
#
# Order: FK checks off, DROP the selected layers' tables, CREATE all tables,
# constraints/ (an FK of the same name is dropped first, so re-runs are
# idempotent), views/ (*_all.sql first), triggers/, FK checks on.
#
# Layer directories are siblings of core/ under sql/. A layer carrying an
# alters/ directory is refused: product layers may not reshape core tables.
#
# Exit codes: 0 ok, 1 forbidden layer content, 2 bad usage.

set -euo pipefail

# Byte-ordered globbing keeps the output identical across platforms.
export LC_ALL=C

SQL_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ $# -eq 0 ]]; then
  echo "usage: build-schema.sh <layer>..." >&2
  exit 2
fi

LAYERS=()
for layer in "$@"; do
  case "${layer}" in
    -h | --help)
      sed -n '2,24p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *[!A-Za-z0-9_-]* | "")
      echo "build-schema.sh: invalid layer name: ${layer}" >&2
      exit 2
      ;;
  esac
  if [[ ! -d "${SQL_ROOT}/${layer}" ]]; then
    echo "build-schema.sh: no such layer: ${SQL_ROOT}/${layer}" >&2
    exit 2
  fi
  if [[ -e "${SQL_ROOT}/${layer}/alters" ]]; then
    echo "build-schema.sh: ${layer}/alters is not allowed; a product layer must not alter core tables" >&2
    exit 1
  fi
  for seen in ${LAYERS[@]+"${LAYERS[@]}"}; do
    if [[ "${seen}" == "${layer}" ]]; then
      echo "build-schema.sh: layer given twice: ${layer}" >&2
      exit 2
    fi
  done
  LAYERS+=("${layer}")
done

# Each helper is a no-op for a missing directory, so a layer may omit any of
# tables/ constraints/ views/ triggers/.

# Table files, layer by layer, alphabetically within each layer. Creation
# order carries no semantics: FK checks are off for the whole CREATE run.
list_tables() {
  local d f
  for d in "$@"; do
    [[ -d "${d}" ]] || continue
    for f in "${d}"/*.sql; do
      [[ -e "${f}" ]] || continue
      printf '%s\n' "${f}"
    done
  done
}

emit_drop_tables() {
  local f
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    echo "DROP TABLE IF EXISTS \`$(basename "${f}" .sql)\`;"
  done < <(list_tables "$@")
}

emit_tables() {
  local f
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    echo "-- >>> $(basename "${f}")"
    cat "${f}"
    echo
  done < <(list_tables "$@")
}

emit_files() {
  local dir="$1" f
  [[ -d "${dir}" ]] || return 0
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    echo "-- >>> $(basename "${f}")"
    cat "${f}"
    echo
  done
}

# Prints "<table> <constraint>" for every ADD CONSTRAINT in the directory.
list_constraint_names() {
  local dir="$1" f
  [[ -d "${dir}" ]] || return 0
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    sed '/^[[:space:]]*--/d' "${f}" | tr '\n\t' '  ' | tr ';' '\n' | awk '
      {
        s = $0
        l = tolower(s)
        if (match(l, /^ *alter +table +`?[a-z0-9_]+`? +add +constraint +`?[a-z0-9_]+`?/)) {
          t = substr(s, RSTART, RLENGTH)
          gsub(/`/, "", t)
          sub(/^ +/, "", t)
          split(t, p, / +/)
          print p[3] " " p[6]
        }
      }'
  done
}

emit_constraints() {
  local dir t n
  local pairs=""
  for dir in "$@"; do
    pairs+="$(list_constraint_names "${dir}")"$'\n'
  done
  if [[ -n "${pairs//$'\n'/}" ]]; then
    cat <<'SQL'
DROP PROCEDURE IF EXISTS nf_drop_fk_if_exists;
DELIMITER $$
CREATE PROCEDURE nf_drop_fk_if_exists(IN p_table VARCHAR(64), IN p_name VARCHAR(64))
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = p_table
      AND CONSTRAINT_NAME = p_name AND CONSTRAINT_TYPE = 'FOREIGN KEY'
  ) THEN
    SET @nf_drop_fk_sql = CONCAT('ALTER TABLE `', p_table, '` DROP FOREIGN KEY `', p_name, '`');
    PREPARE nf_drop_fk_stmt FROM @nf_drop_fk_sql;
    EXECUTE nf_drop_fk_stmt;
    DEALLOCATE PREPARE nf_drop_fk_stmt;
  END IF;
END$$
DELIMITER ;
SQL
    while read -r t n; do
      [[ -n "${t}" ]] || continue
      echo "CALL nf_drop_fk_if_exists('${t}', '${n}');"
    done <<<"${pairs}"
    echo
  fi
  for dir in "$@"; do
    emit_files "${dir}"
  done
  if [[ -n "${pairs//$'\n'/}" ]]; then
    echo "DROP PROCEDURE IF EXISTS nf_drop_fk_if_exists;"
    echo
  fi
}

emit_views() {
  local dir="$1"
  [[ -d "${dir}" ]] || return 0

  # Base views (*_all.sql) load before their dependants; the byte-ordered glob
  # would otherwise put v_x.sql before v_x_all.sql.
  local base_views=() leaf_views=() f
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    if [[ "$(basename "${f}")" == *_all.sql ]]; then
      base_views+=("${f}")
    else
      leaf_views+=("${f}")
    fi
  done

  # Drop leaves first, then bases.
  for f in "${leaf_views[@]:-}" "${base_views[@]:-}"; do
    [[ -n "${f:-}" && -e "${f}" ]] || continue
    echo "DROP VIEW IF EXISTS \`$(basename "${f}" .sql)\`;"
  done
  echo

  for f in "${base_views[@]:-}" "${leaf_views[@]:-}"; do
    [[ -n "${f:-}" && -e "${f}" ]] || continue
    echo "-- >>> $(basename "${f}")"
    cat "${f}"
    echo
  done
}

# Trigger files use DELIMITER $$ ... DELIMITER ; themselves.
emit_triggers() {
  local dir="$1" f
  [[ -d "${dir}" ]] || return 0
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    echo "DROP TRIGGER IF EXISTS \`$(basename "${f}" .sql)\`;"
  done
  echo
  emit_files "${dir}"
}

echo "-- Generated by sql/core/tools/build-schema.sh (layers: ${LAYERS[*]})"
echo "-- DO NOT EDIT the output; edit the files under sql/<layer>/."
echo "SET NAMES utf8mb4;"
echo "SET FOREIGN_KEY_CHECKS = 0;"
echo "SET UNIQUE_CHECKS = 0;"
echo

TABLE_DIRS=()
CONSTRAINT_DIRS=()
for layer in "${LAYERS[@]}"; do
  TABLE_DIRS+=("${SQL_ROOT}/${layer}/tables")
  CONSTRAINT_DIRS+=("${SQL_ROOT}/${layer}/constraints")
done

emit_drop_tables "${TABLE_DIRS[@]}"
echo

emit_tables "${TABLE_DIRS[@]}"

# Cross-layer FKs run only after every selected layer's tables exist.
emit_constraints "${CONSTRAINT_DIRS[@]}"

for layer in "${LAYERS[@]}"; do
  emit_views "${SQL_ROOT}/${layer}/views"
done

for layer in "${LAYERS[@]}"; do
  emit_triggers "${SQL_ROOT}/${layer}/triggers"
done

echo "SET UNIQUE_CHECKS = 1;"
echo "SET FOREIGN_KEY_CHECKS = 1;"
