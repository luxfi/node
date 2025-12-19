#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS] [BUILD_ARGS]

  Build node

  Options:

    -r  Build with race detector

  BUILD_ARGS: Additional arguments to pass to go build (e.g., -tags purego)
"
}

race=''
while getopts 'r' flag; do
  case "${flag}" in
    r)
      echo "Building with race detection enabled"
      race='-race'
      ;;
    *) print_usage
      exit 1 ;;
  esac
done

# Shift to get remaining arguments (build tags, etc.)
shift $((OPTIND-1))

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Configure the build environment
source "${REPO_ROOT}"/scripts/constants.sh
# Determine the git commit hash to use for the build
source "${REPO_ROOT}"/scripts/git_commit.sh

echo "Building Lux Node v${version_major}.${version_minor}.${version_patch} with [$(go version)]..."
go build -trimpath ${race} "$@" -o "${node_path}" \
   -ldflags "-X github.com/luxfi/node/version.GitCommit=$git_commit \
             -X github.com/luxfi/node/version.VersionMajor=$version_major \
             -X github.com/luxfi/node/version.VersionMinor=$version_minor \
             -X github.com/luxfi/node/version.VersionPatch=$version_patch \
             $static_ld_flags" \
   "${REPO_ROOT}"/main
