#!/usr/bin/env bash

set -euo pipefail

# Luxgo root folder
NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$NODE_PATH"/scripts/constants.sh
source "$NODE_PATH"/scripts/git_commit.sh

echo "Building bootstrap-monitor..."
go build -ldflags\
   "-X github.com/luxfi/node/version.GitCommit=$git_commit $static_ld_flags"\
   -o "$NODE_PATH/build/bootstrap-monitor"\
   "$NODE_PATH/tests/fixture/bootstrapmonitor/cmd/"*.go
