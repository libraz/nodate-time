#!/usr/bin/env bash
# check-vendored-core.sh — verify sql/core/ still matches upstream.
#
# sql/core/ is a copy of the shared schema contract, not code this
# repository authors. Two things can go wrong with a copy, and they need
# different checks:
#
#   local  — someone edited the vendored files here. The contract stops
#            describing what both products actually agree on, and the
#            disagreement surfaces as corrupt data rather than a failed
#            build. Checked against the hashes in UPSTREAM.json.
#
#   remote — the reference moved and this copy is stale. Checked by
#            fetching the pinned ref and diffing.
#
# The local check needs nothing but this checkout, so it always runs.
# The remote check needs the upstream repository to be reachable; when
# it is not, this script says so and fails rather than passing quietly,
# because "could not check" and "checked, no drift" must not look alike.
# Pass --local-only to assert just the first (useful offline).
#
# Exit codes:
#   0 — vendored copy is intact and matches the pinned ref
#   1 — drift detected
#   2 — the check could not be performed

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CORE_DIR="$ROOT_DIR/sql/core"
MANIFEST="$CORE_DIR/UPSTREAM.json"

local_only=0
for arg in "$@"; do
  case "$arg" in
    --local-only) local_only=1 ;;
    -h | --help)
      sed -n '2,26p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "$MANIFEST" ]]; then
  echo "ERROR: $MANIFEST not found; the vendored copy has no provenance." >&2
  exit 2
fi

read -r REPO REF <<<"$(python3 -c "
import json
m = json.load(open('$MANIFEST'))
print(m['repository'], m['ref'])
")"

# ── local: the copy has not been edited here ────────────────────────

if ! python3 - "$CORE_DIR" "$MANIFEST" <<'PY'; then
import hashlib, json, os, sys

core, manifest_path = sys.argv[1], sys.argv[2]
manifest = json.load(open(manifest_path))
recorded = {f['path']: f['sha256'] for f in manifest['files']}

present = set()
for dirpath, dirnames, filenames in os.walk(core):
    dirnames.sort()
    for name in sorted(filenames):
        if name in ('UPSTREAM.json', '.DS_Store'):
            continue
        present.add(os.path.relpath(os.path.join(dirpath, name), core))

problems = []
for rel in sorted(recorded):
    full = os.path.join(core, rel)
    if not os.path.exists(full):
        problems.append(f'  missing: {rel}')
        continue
    actual = hashlib.sha256(open(full, 'rb').read()).hexdigest()
    if actual != recorded[rel]:
        problems.append(f'  edited:  {rel}')
for rel in sorted(present - set(recorded)):
    problems.append(f'  added:   {rel}')

if problems:
    print('The vendored contract was changed in this repository:')
    print('\n'.join(problems))
    print('')
    print('sql/core is a copy, not a source. Change it upstream, then')
    print('re-vendor; editing it here makes the two products disagree')
    print('about a schema they both write to.')
    sys.exit(1)
print(f'vendored copy matches its manifest ({len(recorded)} files)')
PY
  exit 1
fi

if [[ $local_only -eq 1 ]]; then
  exit 0
fi

# ── remote: the pinned ref still holds what we vendored ──────────────

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "fetching $REPO at $REF"
if ! git init -q "$TMP_DIR/upstream" \
  || ! git -C "$TMP_DIR/upstream" remote add origin "$REPO" \
  || ! git -C "$TMP_DIR/upstream" fetch -q --depth 1 origin "$REF" 2>"$TMP_DIR/fetch.err"; then
  echo "ERROR: could not fetch $REF from $REPO." >&2
  sed 's/^/  /' "$TMP_DIR/fetch.err" >&2 2>/dev/null || true
  echo "" >&2
  echo "The pinned ref must be reachable for this check to mean anything." >&2
  echo "If the contract has not been pushed upstream yet, that is the" >&2
  echo "thing to fix — not this check. Use --local-only to assert only" >&2
  echo "that the copy is unedited." >&2
  exit 2
fi
git -C "$TMP_DIR/upstream" checkout -q FETCH_HEAD

UPSTREAM_CORE="$TMP_DIR/upstream/sql/core"
if [[ ! -d "$UPSTREAM_CORE" ]]; then
  echo "ERROR: $REF has no sql/core directory." >&2
  exit 2
fi

if diff -ru --exclude=UPSTREAM.json "$UPSTREAM_CORE" "$CORE_DIR" >"$TMP_DIR/core.diff"; then
  echo "sql/core matches $REPO at $REF."
  exit 0
fi

echo "---- sql/core has drifted from $REF ----"
head -n 120 "$TMP_DIR/core.diff"
if [[ "$(wc -l <"$TMP_DIR/core.diff")" -gt 120 ]]; then
  echo "... (truncated)"
fi
exit 1
