#!/usr/bin/env bash
# build-schema.sh
# Concatenate the layered schema into a single stream.
# Pre-release: no migrations. Intended usage is drop & recreate.
#
#   bash sql/build-schema.sh | mysql -h 127.0.0.1 -u root -p nodate_time
#   bash sql/build-schema.sh core | mysql ...   # core layer only
#
# Layers:
#
# Within a layer: tables/, then alters/ (a product layer widening a core
# column it does not own), then constraints/, views/ and triggers/.
#
#   core/   tables shared with any other product implementing the contract:
#           workspaces, users/identities/sessions, calendars, calendar_events
#           and friends, and the append-only `events` log.
#   time/   this product's own tables: the shared photo album, join-by-link
#           calendar invitations, password resets, and the operator-facing
#           OAuth provider configuration.
#
# `core` emits the contract on its own, which is what the conformance
# suite checks. `all` (the default) emits core followed by time, which is
# what this application runs.
#
# FK checks are toggled OFF at the top and ON at the bottom so tables can be
# loaded in plain alphabetical order regardless of dependency direction.
# Cross-layer constraints are emitted after every CREATE TABLE of both
# layers, so their targets always exist by the time they run.

set -euo pipefail

# Pin the collation so filename glob expansion is byte-ordered and identical
# across platforms. Without this, the default locale (e.g. UTF-8 on macOS vs C
# on Linux CI) reorders sibling view files, producing a schema.sql that differs
# between a local run and the CI drift guard.
export LC_ALL=C

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

LAYER_ARG="${1:-all}"
case "${LAYER_ARG}" in
  all)  LAYERS=(core time) ;;
  core) LAYERS=(core) ;;
  *)
    echo "usage: build-schema.sh [all|core]" >&2
    exit 2
    ;;
esac

# ---------------------------------------------------------------------------
# Helpers. Each takes a directory and is a no-op when it does not exist, so a
# layer may omit any of tables/ views/ triggers/ constraints/.
# ---------------------------------------------------------------------------

# list_tables prints one path per table file across the given layer
# directories, layer by layer and alphabetically within each layer. A
# core-only build therefore emits exactly the leading run of a full build.
#
# Creation order carries no semantics: FK checks are off for the whole
# CREATE TABLE run, and no delete path may depend on the order InnoDB walks
# a cascade chain, which follows creation order and is undocumented. A
# teardown that needs a particular row gone first clears it explicitly.
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
  local dir="$1"
  [[ -d "${dir}" ]] || return 0
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    echo "-- >>> $(basename "${f}")"
    cat "${f}"
    echo
  done
}

emit_views() {
  local dir="$1"
  [[ -d "${dir}" ]] || return 0

  # Base views (suffix `_all.sql`) are loaded before their dependants so that
  # filtered child views (e.g. v_task_list, v_task_list_archived) can reference
  # them. Default glob ordering would otherwise put `v_task_list.sql` before
  # `v_task_list_all.sql` because '.' (0x2E) sorts before '_' (0x5F).
  local base_views=() leaf_views=() f
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    if [[ "$(basename "${f}")" == *_all.sql ]]; then
      base_views+=("${f}")
    else
      leaf_views+=("${f}")
    fi
  done

  # Drop in reverse dependency order: leaves first, then bases.
  for f in "${leaf_views[@]:-}" "${base_views[@]:-}"; do
    [[ -n "${f:-}" && -e "${f}" ]] || continue
    echo "DROP VIEW IF EXISTS \`$(basename "${f}" .sql)\`;"
  done
  echo

  # Create in dependency order: bases first, then leaves.
  for f in "${base_views[@]:-}" "${leaf_views[@]:-}"; do
    [[ -n "${f:-}" && -e "${f}" ]] || continue
    echo "-- >>> $(basename "${f}")"
    cat "${f}"
    echo
  done
}

emit_triggers() {
  local dir="$1"
  [[ -d "${dir}" ]] || return 0

  # Triggers are loaded after their tables. Each file is expected to use
  # DELIMITER $$ ... DELIMITER ; internally so the mysql client can parse
  # multi-statement bodies (IF / SIGNAL / etc.). DROP TRIGGER IF EXISTS is
  # emitted up front to keep re-runs idempotent even if the parent table
  # was not dropped between runs.
  local f
  for f in "${dir}"/*.sql; do
    [[ -e "${f}" ]] || continue
    echo "DROP TRIGGER IF EXISTS \`$(basename "${f}" .sql)\`;"
  done
  echo
  emit_files "${dir}"
}

# ---------------------------------------------------------------------------
# Emit
# ---------------------------------------------------------------------------

echo "-- Generated by sql/build-schema.sh (layers: ${LAYERS[*]})"
echo "-- DO NOT EDIT the output; edit files under sql/core/ or sql/time/."
echo "SET NAMES utf8mb4;"
echo "SET FOREIGN_KEY_CHECKS = 0;"
echo "SET UNIQUE_CHECKS = 0;"
echo

# Drop every table across all selected layers before creating any, so a
# re-run is idempotent even when a table moved between layers.
TABLE_DIRS=()
for layer in "${LAYERS[@]}"; do
  TABLE_DIRS+=("${SCRIPT_DIR}/${layer}/tables")
done

emit_drop_tables "${TABLE_DIRS[@]}"
echo

emit_tables "${TABLE_DIRS[@]}"

# A product layer may widen a core table it does not own — an ENUM gaining a
# value this product supports and the shared contract does not name. Keeping
# that as an ALTER here rather than an edit to core/ leaves the vendored
# contract byte-identical to upstream, so the conformance suite still compares
# like for like. Emitted after every CREATE TABLE and before any constraint,
# view or trigger, so the altered column is already in its final shape by the
# time anything references it.
for layer in "${LAYERS[@]}"; do
  emit_files "${SCRIPT_DIR}/${layer}/alters"
done

# Cross-layer foreign keys reference tables from more than one layer, so they
# run only after every layer's CREATE TABLE has been emitted.
for layer in "${LAYERS[@]}"; do
  emit_files "${SCRIPT_DIR}/${layer}/constraints"
done

for layer in "${LAYERS[@]}"; do
  emit_views "${SCRIPT_DIR}/${layer}/views"
done

for layer in "${LAYERS[@]}"; do
  emit_triggers "${SCRIPT_DIR}/${layer}/triggers"
done

echo "SET UNIQUE_CHECKS = 1;"
echo "SET FOREIGN_KEY_CHECKS = 1;"
