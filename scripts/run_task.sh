#!/usr/bin/env bash
set -euo pipefail
if command -v task > /dev/null 2>&1; then
  exec task "${@}"
else
  exec go run github.com/go-task/task/v3/cmd/task@v3.39.2 "${@}"
fi
