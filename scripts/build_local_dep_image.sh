#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building docker image based off of most recent local commits of luxd and coreth"

AVALANCHE_REMOTE="git@github.com:luxdefi/luxd.git"
CORETH_REMOTE="git@github.com:luxdefi/coreth.git"
DOCKERHUB_REPO="avaplatform/luxd"

DOCKER="${DOCKER:-docker}"
SCRIPT_DIRPATH=$(cd $(dirname "${BASH_SOURCE[0]}") && pwd)
ROOT_DIRPATH="$(dirname "${SCRIPT_DIRPATH}")"

AVA_LABS_RELATIVE_PATH="src/github.com/luxdefi"
EXISTING_GOPATH="$GOPATH"

export GOPATH="$SCRIPT_DIRPATH/.build_image_gopath"
WORKPREFIX="$GOPATH/src/github.com/luxdefi"

# Clone the remotes and checkout the desired branch/commits
AVALANCHE_CLONE="$WORKPREFIX/luxd"
CORETH_CLONE="$WORKPREFIX/coreth"

# Replace the WORKPREFIX directory
rm -rf "$WORKPREFIX"
mkdir -p "$WORKPREFIX"


AVALANCHE_COMMIT_HASH="$(git -C "$EXISTING_GOPATH/$AVA_LABS_RELATIVE_PATH/luxd" rev-parse --short HEAD)"
CORETH_COMMIT_HASH="$(git -C "$EXISTING_GOPATH/$AVA_LABS_RELATIVE_PATH/coreth" rev-parse --short HEAD)"

git config --global credential.helper cache

git clone "$AVALANCHE_REMOTE" "$AVALANCHE_CLONE"
git -C "$AVALANCHE_CLONE" checkout "$AVALANCHE_COMMIT_HASH"

git clone "$CORETH_REMOTE" "$CORETH_CLONE"
git -C "$CORETH_CLONE" checkout "$CORETH_COMMIT_HASH"

CONCATENATED_HASHES="$AVALANCHE_COMMIT_HASH-$CORETH_COMMIT_HASH"

"$DOCKER" build -t "$DOCKERHUB_REPO:$CONCATENATED_HASHES" "$WORKPREFIX" -f "$SCRIPT_DIRPATH/local.Dockerfile"
