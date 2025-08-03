#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build_lux [OPTIONS]

  Build node

  Options:

    -r  Build with race detector
"
}

race=''
while getopts 'r' flag; do
  case "${flag}" in
    r) race='-race' ;;
    *) print_usage
      exit 1 ;;
  esac
done

# Lux Node root folder
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

build_args="$race"
echo "Building Lux Node..."
go build $build_args -ldflags "-X github.com/luxfi/node/version.GitCommit=$git_commit $static_ld_flags" -o "$node_path" "$LUX_PATH/main/"*.go
