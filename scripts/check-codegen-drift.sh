#!/usr/bin/env bash
# check-codegen-drift.sh — verify the generated data layer still matches the
# schema and queries it is generated from.
#
# The schema and the code generated from it are two representations of the
# same thing, and only one of them is edited by hand. When a column is added
# and sqlc is not re-run, the generated struct simply lacks that field: the
# build still succeeds, the tests still pass, and the gap only surfaces when
# somebody tries to use the column and cannot. Nothing else in the pipeline
# notices, which is why this check exists.
#
# Column comments count. sqlc copies them into the generated Go doc comments,
# so a comment-only schema edit still leaves the generated files stale.
#
# The check never writes into the working tree: sqlc is pointed at a scratch
# directory and the result is compared against what is committed. A failure
# therefore tells you to run the generator; it does not half-run it for you.
#
# Usage:
#   bash scripts/check-codegen-drift.sh            # check everything
#   bash scripts/check-codegen-drift.sh --staged   # skip when no input is staged
#
# Exit codes:
#   0 — schema and generated code agree with their sources
#   1 — drift detected
#   2 — the check could not be performed

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SQLC_CONFIG="$ROOT_DIR/sql/sqlc.yaml"

# Pinned because two sqlc versions can emit different output from identical
# input; comparing across versions would report drift that is not there.
SQLC_VERSION="v1.30.0"

staged_only=0
for arg in "$@"; do
  case "$arg" in
    --staged) staged_only=1 ;;
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

# ── staged mode: only run when an input to the generators is staged ──

if [[ $staged_only -eq 1 ]]; then
  if ! git -C "$ROOT_DIR" rev-parse --git-dir >/dev/null 2>&1; then
    exit 0
  fi
  if ! git -C "$ROOT_DIR" diff --cached --name-only --diff-filter=ACMR \
    -- 'sql/**' | grep -q .; then
    exit 0
  fi
fi

# ── the composed schema matches the layered sources ──

# build-schema.sh writes to stdout; sql/schema.sql is the committed result of
# redirecting it. Comparing the two costs nothing and needs no generator.
if ! diff -u "$ROOT_DIR/sql/schema.sql" <(bash "$ROOT_DIR/sql/build-schema.sh") >/dev/null; then
  echo "sql/schema.sql is out of date with sql/core/ and sql/time/."
  echo ""
  diff -u "$ROOT_DIR/sql/schema.sql" <(bash "$ROOT_DIR/sql/build-schema.sh") | head -n 40 || true
  echo ""
  echo "Rebuild it alongside the change that moved the layers:"
  echo "  make db-schema"
  exit 1
fi
echo "sql/schema.sql is in sync with sql/core/ and sql/time/"

# ── the generated Go matches the schema and queries ──

if ! command -v sqlc >/dev/null 2>&1; then
  echo "ERROR: sqlc is not installed, so the generated code cannot be checked." >&2
  echo "  go install github.com/sqlc-dev/sqlc/cmd/sqlc@$SQLC_VERSION" >&2
  exit 2
fi

installed="$(sqlc version 2>/dev/null | tr -d '[:space:]')"
if [[ "$installed" != "$SQLC_VERSION" ]]; then
  echo "ERROR: sqlc $installed is installed but this repository generates with $SQLC_VERSION." >&2
  echo "  Different versions emit different code, so a comparison across them" >&2
  echo "  would report drift that does not exist. Install the pinned version:" >&2
  echo "    go install github.com/sqlc-dev/sqlc/cmd/sqlc@$SQLC_VERSION" >&2
  exit 2
fi

SCRATCH="$(mktemp -d)"
# The config lives beside the schema and queries it names by relative path,
# so the copy has to stay in the same directory for those to resolve.
SCRATCH_CONFIG="$(mktemp "$ROOT_DIR/sql/.sqlc-drift-XXXXXX.yaml")"
trap 'rm -rf "$SCRATCH" "$SCRATCH_CONFIG"' EXIT

# Redirect every output directory into the scratch tree, keeping the same
# relative layout so nested outputs stay nested and a recursive diff lines up.
outs="$(python3 - "$SQLC_CONFIG" "$SCRATCH_CONFIG" "$SCRATCH" "$ROOT_DIR" <<'PY'
import os, re, sys

config_path, scratch_config, scratch, root = sys.argv[1:5]
sql_dir = os.path.dirname(config_path)

rewritten = []
outs = []


def repoint(match):
    prefix, quote, value = match.group(1), match.group(2), match.group(3)
    rel = os.path.relpath(os.path.normpath(os.path.join(sql_dir, value)), root)
    outs.append(rel)
    # sqlc joins out: onto the config's directory, so an absolute path would
    # land under sql/ rather than at the root of the filesystem. Express the
    # scratch destination as a path relative to that directory instead.
    dest = os.path.relpath(os.path.join(scratch, rel), sql_dir)
    return f"{prefix}{quote}{dest}{quote}"


with open(config_path) as fh:
    for line in fh:
        rewritten.append(re.sub(r'^(\s*out:\s*)(["\']?)(.*?)\2\s*$', repoint, line.rstrip("\n")) + "\n")

if not outs:
    sys.stderr.write("no out: entries found in the sqlc config\n")
    sys.exit(2)

with open(scratch_config, "w") as fh:
    fh.writelines(rewritten)

print("\n".join(outs))
PY
)"

sqlc generate -f "$SCRATCH_CONFIG" >/dev/null

# Generated output shares a directory with hand-written files (tests,
# .gitattributes), so the comparison is limited to what sqlc actually emits:
# every file it wrote into the scratch tree, plus any file left in the
# repository that carries a generated name but is no longer produced.
if ! python3 - "$ROOT_DIR" "$SCRATCH" <<PY
import difflib, os, sys

root, scratch = sys.argv[1], sys.argv[2]
outs = """$outs""".split()

GENERATED_NAMES = {"models.go", "db.go", "querier.go", "copyfrom.go", "batch.go"}


def is_generated_name(name):
    return name.endswith(".sql.go") or name in GENERATED_NAMES


problems = []
checked = set()

for rel in outs:
    produced = set()
    for dirpath, _, filenames in os.walk(os.path.join(scratch, rel)):
        for name in sorted(filenames):
            full = os.path.join(dirpath, name)
            key = os.path.relpath(full, scratch)
            produced.add(key)
            if key in checked:
                continue
            checked.add(key)
            committed = os.path.join(root, key)
            if not os.path.exists(committed):
                problems.append((key, ["  the generator produces this file, but it is not committed"]))
                continue
            new = open(full, encoding="utf-8").read().splitlines(keepends=True)
            old = open(committed, encoding="utf-8").read().splitlines(keepends=True)
            if new != old:
                diff = list(difflib.unified_diff(old, new, "committed", "regenerated", n=2))
                problems.append((key, ["  " + line.rstrip("\n") for line in diff[:40]]))

    for dirpath, _, filenames in os.walk(os.path.join(root, rel)):
        for name in sorted(filenames):
            if not is_generated_name(name):
                continue
            key = os.path.relpath(os.path.join(dirpath, name), root)
            if key not in produced and key not in checked:
                checked.add(key)
                problems.append((key, ["  no longer produced by the generator; it is stale"]))

if problems:
    print("The generated data layer no longer matches the schema it comes from.")
    print("")
    for key, lines in problems:
        print(f"---- {key} ----")
        print("\n".join(lines))
    sys.exit(1)
PY
then
  echo ""
  echo "Run the generator and commit its output alongside the schema change:"
  echo "  make sqlc"
  exit 1
fi

echo "generated data layer matches the schema and queries"
