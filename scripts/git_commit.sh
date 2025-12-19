#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# WARNING: this will use the most recent commit even if there are un-committed changes present
git_commit="${LUXD_COMMIT:-$(git --git-dir="${LUX_PATH}/.git" rev-parse HEAD)}"
commit_hash="${git_commit::8}"

# Extract version from git tag - always use git, never fallback to hardcoded
# Examples: v1.22.19 -> 1.22.19, v1.22.19-0-g7dc749f -> 1.22.19
git_raw_version="${LUXD_VERSION:-$(git --git-dir="${LUX_PATH}/.git" describe --tags --always 2>/dev/null)}"

# Strip leading 'v' if present
git_raw_version="${git_raw_version#v}"

# Extract just the semver part (Major.Minor.Patch) - strips anything after patch number
# Handles: 1.22.19, 1.22.19-0-g7dc749f, 1.22.19-beta, etc.
if [[ "$git_raw_version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    version_major="${BASH_REMATCH[1]}"
    version_minor="${BASH_REMATCH[2]}"
    version_patch="${BASH_REMATCH[3]}"
else
    echo "ERROR: Git tag '$git_raw_version' is not semantic version format (X.Y.Z)"
    echo "Please tag the repository with a proper semver tag: git tag v1.22.20"
    exit 1
fi

git_version="${version_major}.${version_minor}.${version_patch}"
