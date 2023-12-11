#!/usr/bin/env bash

set -euo pipefail

# Luxgo root folder
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

echo "Building tmpnetctl..."
go build -ldflags\
   "-X github.com/luxdefi/node/version.GitCommit=$git_commit $static_ld_flags"\
   -o "$LUX_PATH/build/tmpnetctl"\
   "$LUX_PATH/tests/fixture/tmpnet/cmd/"*.go
