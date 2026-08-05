#!/usr/bin/env bash
#
# Every error this API can return must have a message the reader can read.
#
# An error code with no entry in the client dictionaries falls back to the
# server's own English sentence, which then appears verbatim in a Japanese
# interface -- and only for the failures nobody exercised in development, which
# is to say the rarer and more alarming ones.
#
# This lives here rather than in a Go test because the dictionaries sit outside
# the Go module: the test cache does not track them, so an edit to a dictionary
# alone would leave a stale pass behind.
set -euo pipefail

cd "$(dirname "$0")/.."

SOURCE=apps/api/internal/errors/errors.go
# The Japanese dictionary is the authoritative key set: the English one is
# typed against it, so a key present there is present in both.
DICT=apps/web/src/i18n/ja.ts

declared=$(grep -c '&Spec{' "$SOURCE")
codes=$(grep -oE '&Spec\{[[:space:]]*Status:[^,]+,[[:space:]]*Code:[[:space:]]*"[A-Z_.]+"' "$SOURCE" \
  | grep -oE '"[A-Z_.]+"$' | tr -d '"')
recognised=$(printf '%s\n' "$codes" | grep -c . || true)

if [ "$declared" -ne "$recognised" ]; then
  echo "error: $declared error definitions declared but $recognised recognised in $SOURCE" >&2
  echo "       the unrecognised ones would be silently exempt from this check" >&2
  exit 1
fi

missing=""
while IFS= read -r code; do
  [ -n "$code" ] || continue
  if ! grep -q "'apiError.$code'" "$DICT"; then
    missing="$missing $code"
  fi
done <<<"$codes"

if [ -n "$missing" ]; then
  echo "error: no client message for:$missing" >&2
  echo "       add an 'apiError.<CODE>' entry to apps/web/src/i18n/{ja,en}.ts" >&2
  exit 1
fi

# A duplicate code makes one of the two unreachable to a client branching on
# it, and the message shown is whichever definition happened to win.
dupes=$(printf '%s\n' "$codes" | sort | uniq -d)
if [ -n "$dupes" ]; then
  echo "error: error code defined more than once: $dupes" >&2
  exit 1
fi

echo "every error code has a client message ($recognised codes)"
