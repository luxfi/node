#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$LUX_PATH"/scripts/constants.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$LUX_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

echo "Building Docker Image with tags: $node_dockerhub_repo:$commit_hash , $node_dockerhub_repo:$current_branch"
docker build -t "$node_dockerhub_repo:$commit_hash" \
        -t "$node_dockerhub_repo:$current_branch" "$LUX_PATH" -f "$LUX_PATH/Dockerfile"
