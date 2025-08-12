#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

echo "Building Workload..."
go build -o "$LUX_PATH/build/antithesis-xsvm-workload" "$LUX_PATH/tests/antithesis/xsvm/"*.go
