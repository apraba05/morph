#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
export PORT="${PORT:-8000}"

if command -v go >/dev/null 2>&1; then
  exec go run .
fi

echo "Go 1.22+ is required to run this demo." >&2
exit 1
