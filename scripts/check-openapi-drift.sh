#!/usr/bin/env bash
# Fail when apps/api/openapi.json differs from the document the router builds.
# The document is regenerated into a scratch file; the working tree is not
# written. Fix a failure with `make openapi`.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
committed="$root/apps/api/openapi.json"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

(cd "$root/apps/api" && go run ./cmd/openapi -o "$tmp")
if ! cmp -s "$committed" "$tmp"; then
  echo "check-openapi-drift: apps/api/openapi.json is out of date; run 'make openapi'" >&2
  exit 1
fi
