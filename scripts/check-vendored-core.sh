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
#   pin    — the copy no longer matches the commit it claims to come
#            from, which is what a rewritten upstream history looks like.
#            Checked by fetching the pinned ref and diffing.
#
#   behind — upstream changed the contract and this copy was never
#            re-vendored. Nothing local can detect this: the pin still
#            validates perfectly, because the pin is what went stale.
#            Checked against upstream's default branch.
#
# The local check needs nothing but this checkout, so it always runs.
# The other two need the upstream repository to be reachable; when it is
# not, this script says so and fails rather than passing quietly, because
# "could not check" and "checked, no drift" must not look alike.
# Pass --local-only to assert just the first (useful offline).
#
# Exit codes:
#   0 — vendored copy is intact, matches the pin, and the pin is current
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

# ── pin: the pinned ref still holds what we vendored ─────────────────

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

# The manifest records the URL a person clones with, which may be SSH.
# Fetching is read-only and the upstream repository is public, so an
# agent without a key (CI, most notably) can still do it over HTTPS.
https_url() {
  printf '%s' "$1" | sed -E 's#^git@([^:]+):#https://\1/#'
}

fetch_into() {
  local dir="$1" what="$2"
  git init -q "$dir" || return 1
  git -C "$dir" remote add origin "$REPO" || return 1
  if git -C "$dir" fetch -q --depth 1 origin "$what" 2>"$TMP_DIR/fetch.err"; then
    return 0
  fi
  local alt
  alt="$(https_url "$REPO")"
  if [[ "$alt" != "$REPO" ]]; then
    git -C "$dir" remote set-url origin "$alt" || return 1
    git -C "$dir" fetch -q --depth 1 origin "$what" 2>"$TMP_DIR/fetch.err" && return 0
  fi
  return 1
}

echo "fetching $REPO at $REF"
if ! fetch_into "$TMP_DIR/upstream" "$REF"; then
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

if ! diff -ru --exclude=UPSTREAM.json "$UPSTREAM_CORE" "$CORE_DIR" >"$TMP_DIR/core.diff"; then
  echo "---- sql/core has drifted from $REF ----"
  head -n 120 "$TMP_DIR/core.diff"
  if [[ "$(wc -l <"$TMP_DIR/core.diff")" -gt 120 ]]; then
    echo "... (truncated)"
  fi
  exit 1
fi
echo "sql/core matches $REPO at $REF."

# ── behind: the pin itself is what went stale ────────────────────────
#
# Everything above validates a claim: that this copy equals a particular
# commit. It stays true forever, including long after upstream changed
# the contract, because the pin is the thing that goes out of date. The
# only way to notice is to look at what upstream says today.

DEFAULT_REF="${UPSTREAM_DEFAULT_BRANCH:-main}"

echo "fetching $REPO at $DEFAULT_REF"
if ! fetch_into "$TMP_DIR/head" "$DEFAULT_REF"; then
  echo "ERROR: could not fetch $DEFAULT_REF from $REPO." >&2
  sed 's/^/  /' "$TMP_DIR/fetch.err" >&2 2>/dev/null || true
  echo "" >&2
  echo "Whether the pin is current cannot be answered without it." >&2
  exit 2
fi
git -C "$TMP_DIR/head" checkout -q FETCH_HEAD
HEAD_SHA="$(git -C "$TMP_DIR/head" rev-parse HEAD)"

if diff -ru --exclude=UPSTREAM.json "$TMP_DIR/head/sql/core" "$CORE_DIR" >"$TMP_DIR/head.diff"; then
  echo "sql/core is current with $DEFAULT_REF ($HEAD_SHA)."
  exit 0
fi

echo "---- the contract moved upstream and this copy was not re-vendored ----"
echo "pinned:  $REF"
echo "current: $HEAD_SHA ($DEFAULT_REF)"
echo ""
head -n 120 "$TMP_DIR/head.diff"
if [[ "$(wc -l <"$TMP_DIR/head.diff")" -gt 120 ]]; then
  echo "... (truncated)"
fi
echo ""
echo "Re-vendor sql/core from $DEFAULT_REF and update UPSTREAM.json, or"
echo "decide deliberately to stay behind and move the pin when you do."
exit 1
