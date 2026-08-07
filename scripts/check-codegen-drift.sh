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
# A second gap runs the other way: a query is written, generated, and then
# nothing ever calls it. The endpoint it was written for is missing, and
# because the generated method compiles on its own nothing says so. The
# last section of this script lists those, with an allow-list for the ones
# whose absent caller is the current answer.
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
tool_only=0
for arg in "$@"; do
  case "$arg" in
    --staged) staged_only=1 ;;
    --check-tool) tool_only=1 ;;
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

# Generating with the wrong version rewrites every generated file, which
# reads as a huge legitimate-looking diff rather than as a mistake. The
# generate targets assert this before running, so the mistake is refused
# instead of reviewed.
assert_sqlc_version() {
  if ! command -v sqlc >/dev/null 2>&1; then
    echo "ERROR: sqlc is not installed." >&2
    echo "  go install github.com/sqlc-dev/sqlc/cmd/sqlc@$SQLC_VERSION" >&2
    exit 2
  fi
  local installed
  installed="$(sqlc version 2>/dev/null | tr -d '[:space:]')"
  if [[ "$installed" != "$SQLC_VERSION" ]]; then
    echo "ERROR: sqlc $installed is installed but this repository generates with $SQLC_VERSION." >&2
    echo "  Different versions emit different code from identical input." >&2
    echo "    go install github.com/sqlc-dev/sqlc/cmd/sqlc@$SQLC_VERSION" >&2
    exit 2
  fi
}

if [[ $tool_only -eq 1 ]]; then
  assert_sqlc_version
  echo "sqlc $SQLC_VERSION"
  exit 0
fi

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

# A comparison across versions would report drift that is not there, so
# this must hold before the scratch generation means anything.
assert_sqlc_version

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

# ── every generated query has a caller ──

# The largest category of defect in this codebase has been the wiring gap:
# a query written and generated for an endpoint that was never finished.
# The generated method compiles whether or not anything calls it, so the
# gap survives review, the build, and the tests, and surfaces only when
# somebody uses the feature and finds nothing there.
#
# A call from a test counts. A query exercised only by a test is wired to
# something, and treating it as dead would push the next reader to delete
# a tested query rather than to finish the endpoint.
unused_status=0
python3 - "$ROOT_DIR" <<PY || unused_status=$?
import os, re, sys

root = sys.argv[1]
outs = """$outs""".split()

GENERATED_NAMES = {"models.go", "db.go", "querier.go", "copyfrom.go", "batch.go"}

# Methods nothing calls, and why that is the current answer rather than an
# oversight. Two kinds of entry live here, and the wording says which:
# a query deliberately left uncalled, and a query whose caller has not
# been written yet. The second kind records outstanding work — it leaves
# this list when the endpoint is wired, not when the list gets long.
ALLOWED = {
    # Emitted by sqlc onto Queries in db.go; not a query. The transaction
    # helper builds its Queries with generated.New(tx), so nothing reaches
    # for it.
    "WithTx",

    # Superseded by CountCalendarOwnersForUpdate. The last-owner guard has
    # to hold its count across the role change or two concurrent
    # demotions both pass, so the locking variant is the only one a
    # caller should want.
    "CountCalendarOwners",
    # Locking variants of reads whose callers do not yet take the lock:
    # the membership write reads through GetCalendarMember and the
    # password change through GetLocalIdentityByUser. Wiring is
    # outstanding, and deleting these would remove the fix rather than
    # the problem.
    "GetCalendarMemberForUpdate",
    "GetLocalIdentityByUserForUpdate",

    # No caller yet. Expired verification rows accumulate: the cleanup
    # sweeps cover password resets, signin states, sessions and storage,
    # and this table was left out of the loop.
    "DeleteExpiredEmailVerifications",

    # No endpoint yet. Instance admin can be granted from the createuser
    # command, but nothing in the API lists the grants or takes one back,
    # which leaves an admin removable only by hand in SQL.
    "ListInstanceAdmins",
    "RevokeInstanceAdmin",
    # No account screen yet. Listing a user's linked identities and
    # disabling one are the read and write halves of a connected-accounts
    # page that has not been built.
    "ListIdentitiesForUser",
    "DisableIdentity",

    # Superseded by RevokeInviteByPublicIDAndCalendar, which the delete
    # endpoint calls. Revoking by internal id alone carries no calendar
    # scope, so the authorization would have to be repeated outside the
    # query — the thing the resolve helpers exist to prevent.
    "RevokeInvite",

    # The admin OAuth screen reads one provider at a time through
    # GetOAuthProviderConfig, and writes the enabled flag through
    # UpsertOAuthProviderConfig. Both of these are the shapes that screen
    # would use if it listed providers or toggled one on its own.
    "ListOAuthProviderConfigs",
    "SetOAuthProviderEnabled",

    # This deployment is single-tenant: the workspace is resolved once at
    # startup by slug, and every handler carries the id it got. Reading a
    # workspace by id, or checking workspace membership, are the shapes a
    # multi-tenant writer on this schema needs; neither has a question to
    # answer here.
    "GetWorkspaceByID",
    "GetWorkspaceMember",

    # The tail side of the append-only log, which another process on this
    # database reads. This application is the writer; the query is here
    # because the log is a shared channel, not because a handler wants it.
    "ListEventsSince",

}


def is_generated_name(name):
    return name.endswith(".sql.go") or name in GENERATED_NAMES


generated_files = []
caller_files = []
generated_dirs = tuple(os.path.join(root, rel) for rel in outs)

for dirpath, dirnames, filenames in os.walk(root):
    dirnames[:] = [d for d in dirnames if d not in {".git", "node_modules", "vendor"}]
    for name in sorted(filenames):
        if not name.endswith(".go"):
            continue
        full = os.path.join(dirpath, name)
        if dirpath.startswith(generated_dirs) and is_generated_name(name):
            generated_files.append(full)
        else:
            caller_files.append(full)

if not generated_files:
    print("no generated Go files found under the sqlc output directories", file=sys.stderr)
    sys.exit(2)

methods = set()
for path in generated_files:
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            match = re.match(r"func \(q \*Queries\) ([A-Z]\w*)\(", line)
            if match:
                methods.add(match.group(1))

sources = "\n".join(open(path, encoding="utf-8").read() for path in caller_files)
called = {name for name in methods if re.search(r"\.\s*" + name + r"\(", sources)}
uncalled = methods - called

unlisted = sorted(uncalled - ALLOWED)
# An allow-listed method that acquired a caller has to leave the list, or
# the reasons above stop describing the code and the next reader has no
# way to tell which of them still hold.
stale = sorted(ALLOWED & called)

if unlisted or stale:
    if unlisted:
        print("Generated queries that nothing calls:")
        print("")
        for name in unlisted:
            print(f"  {name}")
        print("")
        print("Each one is a query with no endpoint behind it. Wire it up, or")
        print("add it to ALLOWED in this script with the reason it stays.")
    if stale:
        if unlisted:
            print("")
        print("Allow-listed as uncalled, but now called:")
        print("")
        for name in stale:
            print(f"  {name}")
        print("")
        print("Remove these from ALLOWED so the remaining reasons stay true.")
    sys.exit(1)

print(f"every generated query has a caller ({len(called)} called, {len(uncalled)} allow-listed)")
PY

# 1 for an unused query, 2 for a check that could not run at all: the two
# mean different things to a caller and the wrapper must not flatten them.
if [[ $unused_status -ne 0 ]]; then
  exit "$unused_status"
fi
