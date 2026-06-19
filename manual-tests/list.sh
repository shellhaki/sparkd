#!/usr/bin/env sh
set -eu

SPARKD_URL="${SPARKD_URL:-http://127.0.0.1:8721}"

curl "$SPARKD_URL/list"
