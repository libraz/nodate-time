#!/usr/bin/env bash
# Fail when the committed web API types differ from what the OpenAPI document
# generates, or when web code reaches the API around the typed client.
#
# OPENAPI_JSON and SCHEMA_DTS override the document and the committed output.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
spec="${OPENAPI_JSON:-$root/apps/api/openapi.json}"
committed="${SCHEMA_DTS:-$root/apps/web/src/lib/api-schema.d.ts}"
src="$root/apps/web/src"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

"$root/tools/openapi-ts/gen.sh" "$spec" "$tmp" >/dev/null
if ! diff -u "$committed" "$tmp"; then
  echo "check-web-api-types: ${committed#"$root"/} is out of date; run 'make openapi'" >&2
  exit 1
fi

# Everything outside the typed client, its mock and the generated file must go
# through the client. lib/upload.ts PUTs to a signed object-storage URL and
# lib/build-check.ts reads the SPA's own index.html; neither is the API.
bypass="$(grep -rnE --include='*.ts' --include='*.tsx' \
  -e '\bfetch\(' -e "lib/api['\"]" -e '^\s*(export )?(interface|type) \w+(Request|Response)\b' "$src" |
  grep -vE '/(lib/client\.ts|lib/upload\.ts|lib/build-check\.ts|lib/api-schema\.d\.ts|test/mock-client\.ts):|\.test\.tsx?:' || true)"
if [ -n "$bypass" ]; then
  echo "$bypass" >&2
  echo "check-web-api-types: web code bypasses the typed client" >&2
  exit 1
fi
