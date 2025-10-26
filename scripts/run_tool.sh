#!/usr/bin/env bash

set -euo pipefail

LUX_PATH="$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
go tool -modfile="${LUX_PATH}"/tools/go.mod "${@}"
