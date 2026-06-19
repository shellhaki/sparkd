#!/usr/bin/env sh
set -eu

SPARKD_URL="${SPARKD_URL:-http://127.0.0.1:8721}"
PAYLOAD="$(dirname "$0")/create-pg.json"

curl \
  -X POST "$SPARKD_URL/create" \
  -H 'content-type: application/json' \
  -d @"$PAYLOAD"
