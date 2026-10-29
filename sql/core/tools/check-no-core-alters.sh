#!/usr/bin/env bash
# check-no-core-alters.sh — fail when a product layer reshapes a core table.
#
#   bash sql/core/tools/check-no-core-alters.sh [<sql-root>]
#
# <sql-root> is the directory holding core/ and the product layers (default:
# the sql/ directory this script lives under). A product layer is any other
# directory there with tables/, alters/, constraints/, views/ or triggers/.
#
# Reported, exit 1:
#   - ALTER TABLE or CREATE INDEX on a core table in a product layer's
#     tables/, alters/, views/ or triggers/
#   - a clause other than ADD CONSTRAINT ... FOREIGN KEY in an ALTER TABLE of
#     a core table under constraints/
# Core tables are the basenames of <sql-root>/core/tables/*.sql.
#
# Exit codes: 0 clean, 1 violation, 2 bad usage.

set -euo pipefail
export LC_ALL=C

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,19p' "${BASH_SOURCE[0]}"
  exit 0
fi
if [[ $# -gt 1 ]]; then
  echo "usage: check-no-core-alters.sh [<sql-root>]" >&2
  exit 2
fi

ROOT="${1:-$(dirname "${BASH_SOURCE[0]}")/../..}"
if [[ ! -d "${ROOT}/core/tables" ]]; then
  echo "check-no-core-alters.sh: ${ROOT}/core/tables not found" >&2
  exit 2
fi
ROOT="$(cd "${ROOT}" && pwd)"

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

for f in "${ROOT}"/core/tables/*.sql; do
  [[ -e "${f}" ]] || continue
  basename "${f}" .sql | tr 'A-Z' 'a-z'
done | sort >"${TMP}/core"

# statements FILE: one lower-cased, comment-free, whitespace-squeezed
# statement per line, backticks removed.
statements() {
  sed '/^[[:space:]]*--/d' "$1" | tr '\n\t' '  ' | tr ';' '\n' \
    | tr 'A-Z' 'a-z' | tr -d '`' | tr -s ' '
}

is_core() { grep -Fxq -- "$1" "${TMP}/core"; }

violations=0
report() {
  echo "$1: $2"
  violations=1
}

ALTER_RE='(^|[^a-z0-9_])alter table (if exists )?([a-z0-9_]+)'
INDEX_RE='(^|[^a-z0-9_])create ((unique|fulltext|spatial) )?index [a-z0-9_]+ (using [a-z]+ )?on ([a-z0-9_]+)'
FK_CLAUSE_RE='^ *add constraint [a-z0-9_]+ foreign key'

for layer_dir in "${ROOT}"/*/; do
  layer="$(basename "${layer_dir}")"
  [[ "${layer}" != "core" ]] || continue
  [[ -d "${layer_dir}tables" || -d "${layer_dir}alters" || -d "${layer_dir}constraints" \
    || -d "${layer_dir}views" || -d "${layer_dir}triggers" ]] || continue

  for sub in tables alters views triggers; do
    for f in "${layer_dir}${sub}"/*.sql; do
      [[ -e "${f}" ]] || continue
      rel="${layer}/${sub}/$(basename "${f}")"
      while IFS= read -r stmt; do
        if [[ "${stmt}" =~ ${ALTER_RE} ]] && is_core "${BASH_REMATCH[3]}"; then
          report "${rel}" "ALTER TABLE on core table ${BASH_REMATCH[3]}"
        fi
        if [[ "${stmt}" =~ ${INDEX_RE} ]] && is_core "${BASH_REMATCH[5]}"; then
          report "${rel}" "CREATE INDEX on core table ${BASH_REMATCH[5]}"
        fi
      done < <(statements "${f}")
    done
  done

  for f in "${layer_dir}constraints"/*.sql; do
    [[ -e "${f}" ]] || continue
    rel="${layer}/constraints/$(basename "${f}")"
    while IFS= read -r stmt; do
      if [[ "${stmt}" =~ ${INDEX_RE} ]] && is_core "${BASH_REMATCH[5]}"; then
        report "${rel}" "CREATE INDEX on core table ${BASH_REMATCH[5]}"
      fi
      [[ "${stmt}" =~ ${ALTER_RE} ]] || continue
      table="${BASH_REMATCH[3]}"
      is_core "${table}" || continue
      # Drop the ALTER TABLE prefix and any parenthesised groups, then every
      # comma-separated clause must be ADD CONSTRAINT ... FOREIGN KEY.
      rest="${stmt#*alter table }"
      rest="${rest#if exists }"
      rest="${rest#"${table}"}"
      while [[ "${rest}" == *"("*")"* ]]; do
        rest="$(printf '%s' "${rest}" | sed -E 's/\([^()]*\)//g')"
      done
      while IFS= read -r clause; do
        [[ "${clause}" =~ ${FK_CLAUSE_RE} ]] \
          || report "${rel}" "ALTER TABLE ${table}: only ADD CONSTRAINT ... FOREIGN KEY is allowed, found:${clause}"
      done < <(printf '%s\n' "${rest}" | tr ',' '\n')
    done < <(statements "${f}")
  done
done

if [[ ${violations} -ne 0 ]]; then
  echo "" >&2
  echo "A product layer must not change the shape of a core table; put the change in core." >&2
  exit 1
fi
echo "no product layer alters a core table"
