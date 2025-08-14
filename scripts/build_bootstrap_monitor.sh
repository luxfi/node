#!/usr/bin/env bash

set -euo pipefail

# Luxd root folder
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh
source "$LUX_PATH"/scripts/git_commit.sh

echo "Building bootstrap-monitor..."
go build -ldflags\
   "-X github.com/luxfi/luxd/version.GitCommit=$git_commit $static_ld_flags"\
   -o "$LUX_PATH/build/bootstrap-monitor"\
   "$LUX_PATH/tests/fixture/bootstrapmonitor/cmd/"*.go
