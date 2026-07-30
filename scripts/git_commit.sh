#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# WARNING: this will use the most recent commit even if there are un-committed changes present
#
# The fallback must not be fatal. Under `set -euo pipefail` a bare `git rev-parse`
# kills the whole build when there is no .git — which is exactly the case for a
# BuildKit git-context build (`git://…#<ref>` checks out a WORKTREE with no .git),
# and it took the Dockerfile down at step 13/19 with
#   fatal: not a git repository: '/build/.git'
# even though the image only needs this string to stamp provenance. The version
# lookup immediately below already tolerates a missing .git the same way; this
# just makes the commit lookup consistent with it.
#
# Pass LUXD_COMMIT to keep a real hash in git-less contexts.
git_commit="${LUXD_COMMIT:-$(git --git-dir="${NODE_PATH}/.git" rev-parse HEAD 2>/dev/null || echo unknown)}"
commit_hash="${git_commit::8}"

# Extract version from git tag - try git first, then fallback to version file
# Examples: v1.22.19 -> 1.22.19, v1.22.19-0-g7dc749f -> 1.22.19
git_raw_version="${LUXD_VERSION:-$(git --git-dir="${NODE_PATH}/.git" describe --tags --always 2>/dev/null || echo "")}"

# Strip leading 'v' if present
git_raw_version="${git_raw_version#v}"

# Extract just the semver part (Major.Minor.Patch) - strips anything after patch number
# Handles: 1.22.19, 1.22.19-0-g7dc749f, 1.22.19-beta, etc.
if [[ "$git_raw_version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    version_major="${BASH_REMATCH[1]}"
    version_minor="${BASH_REMATCH[2]}"
    version_patch="${BASH_REMATCH[3]}"
elif [[ -f "${NODE_PATH}/version.txt" ]]; then
    # Fallback to version.txt file for CI builds without tags
    version_content=$(cat "${NODE_PATH}/version.txt")
    if [[ "$version_content" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
        version_major="${BASH_REMATCH[1]}"
        version_minor="${BASH_REMATCH[2]}"
        version_patch="${BASH_REMATCH[3]}"
    else
        echo "ERROR: VERSION file content '$version_content' is not semantic version format (X.Y.Z)"
        exit 1
    fi
else
    # Default version for development/CI builds without git tags
    echo "WARNING: No git tag found and no VERSION file - using default 0.0.0-dev"
    version_major="0"
    version_minor="0"
    version_patch="0"
fi

git_version="${version_major}.${version_minor}.${version_patch}"
