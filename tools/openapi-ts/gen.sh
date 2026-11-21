#!/usr/bin/env bash
# Generate TypeScript types from an OpenAPI document.
# Usage: gen.sh <openapi.json> <out.d.ts>
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <openapi.json> <out.d.ts>" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
in="$1"
out="$2"

node --import "$root/tools/openapi-ts/runtime.mjs" \
  "$root/node_modules/openapi-typescript/bin/cli.js" "$in" -o "$out"
