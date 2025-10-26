#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

echo "Building Workload..."
go build -o "$LUX_PATH/build/antithesis-node-workload" "$LUX_PATH/tests/antithesis/node/"*.go
