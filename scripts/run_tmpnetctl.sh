#!/usr/bin/env bash

set -euo pipefail

LUX_PATH=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${LUX_PATH}"

# Build the binary before execution to ensure it is always up-to-date. Faster than `go run`.
./scripts/build_tmpnetctl.sh
./build/tmpnetctl "${@}"
