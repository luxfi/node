#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# If this is not a trusted build (Docker Credentials are not set)
if [[ -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

# Lux root directory
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the constants and image tag
source "$LUX_PATH"/scripts/constants.sh
source "$LUX_PATH"/scripts/image_tag.sh

# Check if the image exists locally before trying to push
if ! docker image inspect "$node_dockerhub_repo:$image_tag" > /dev/null 2>&1; then
  echo "WARNING: Image $node_dockerhub_repo:$image_tag not found locally."
  echo "Skipping publish. The image may have been pushed during build."
  exit 0
fi

if [[ $image_tag == "master" || $image_tag == "main" ]]; then
  echo "Tagging current node image as $node_dockerhub_repo:latest"
  docker tag $node_dockerhub_repo:$image_tag $node_dockerhub_repo:latest
fi

echo "Pushing: $node_dockerhub_repo:$image_tag"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

## pushing image with tags
docker image push -a $node_dockerhub_repo
