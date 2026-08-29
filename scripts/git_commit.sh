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

# The version is version/version.txt and nothing else. The Go build embeds that
# same file (version/constants.go), so a plain `go build`, a `go run`, a test
# binary and a released luxd all report one number. This used to be read from
# `git describe` here and hardcoded a second time in version/constants.go, and
# the two had drifted 142 patch versions apart.
node_version="$(tr -d '[:space:]' < "${NODE_PATH}/version/version.txt")"
