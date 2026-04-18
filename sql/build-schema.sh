#!/usr/bin/env bash
# Concatenate all table definitions into a single schema.sql
set -euo pipefail
cd "$(dirname "$0")"

{
  echo "-- Auto-generated schema. Do not edit directly."
  echo "-- Run: bash sql/build-schema.sh"
  echo ""
  for f in tables/*.sql; do
    echo "-- ===== $f ====="
    cat "$f"
    echo ""
  done
} > schema.sql

echo "Generated schema.sql"
