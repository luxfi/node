#!/usr/bin/env bash

set -euo pipefail

# Builds docker images for antithesis testing.

# e.g.,
# TEST_SETUP=luxd ./scripts/build_antithesis_images.sh                                          # Build local images for luxd
# TEST_SETUP=luxd NODE_ONLY=1 ./scripts/build_antithesis_images.sh                              # Build only a local node image for luxd
# TEST_SETUP=xsvm ./scripts/build_antithesis_images.sh                                                 # Build local images for xsvm
# TEST_SETUP=xsvm IMAGE_PREFIX=<registry>/<repo> IMAGE_TAG=latest ./scripts/build_antithesis_images.sh # Specify a prefix to enable image push and use a specific tag

TEST_SETUP="${TEST_SETUP:-}"
if [[ "${TEST_SETUP}" != "luxd" && "${TEST_SETUP}" != "xsvm" ]]; then
  echo "TEST_SETUP must be set. Valid values are 'luxd' or 'xsvm'"
  exit 255
fi

# Directory above this script
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

source "${LUX_PATH}"/scripts/constants.sh
source "${LUX_PATH}"/scripts/git_commit.sh

# Import common functions used to build images for antithesis test setups
source "${LUX_PATH}"/scripts/lib_build_antithesis_images.sh

# Specifying an image prefix will ensure the image is pushed after build
IMAGE_PREFIX="${IMAGE_PREFIX:-}"

IMAGE_TAG="${IMAGE_TAG:-}"
if [[ -z "${IMAGE_TAG}" ]]; then
  # Default to tagging with the commit hash
  IMAGE_TAG="${commit_hash}"
fi

# The dockerfiles don't specify the golang version to minimize the changes required to bump
# the version. Instead, the golang version is provided as an argument.
GO_VERSION="$(go list -m -f '{{.GoVersion}}')"

# Helper to simplify calling build_builder_image for test setups in this repo
function build_builder_image_for_luxd {
  echo "Building builder image"
  build_antithesis_builder_image "${GO_VERSION}" "antithesis-luxd-builder:${IMAGE_TAG}" "${LUX_PATH}" "${LUX_PATH}"
}

# Helper to simplify calling build_antithesis_images for test setups in this repo
function build_antithesis_images_for_luxd {
  local test_setup=$1
  local image_prefix=$2
  local uninstrumented_node_dockerfile=$3
  local node_only=${4:-}

  if [[ -z "${node_only}" ]]; then
    echo "Building node image for ${test_setup}"
  else
    echo "Building images for ${test_setup}"
  fi
  build_antithesis_images "${GO_VERSION}" "${image_prefix}" "antithesis-${test_setup}" "${IMAGE_TAG}" "${IMAGE_TAG}" \
                          "${LUX_PATH}/tests/antithesis/${test_setup}/Dockerfile" "${uninstrumented_node_dockerfile}" \
                          "${LUX_PATH}" "${node_only}" "${git_commit}"
}

if [[ "${TEST_SETUP}" == "luxd" ]]; then
  build_builder_image_for_luxd

  echo "Generating compose configuration for ${TEST_SETUP}"
  gen_antithesis_compose_config "${IMAGE_TAG}" "${LUX_PATH}/tests/antithesis/luxd/gencomposeconfig" \
                                "${LUX_PATH}/build/antithesis/luxd"

  build_antithesis_images_for_luxd "${TEST_SETUP}" "${IMAGE_PREFIX}" "${LUX_PATH}/Dockerfile" "${NODE_ONLY:-}"
else
  build_builder_image_for_luxd

  # Only build the luxd node image to use as the base for the xsvm image. Provide an empty
  # image prefix (the 1st argument) to prevent the image from being pushed
  NODE_ONLY=1
  build_antithesis_images_for_luxd luxd "" "${LUX_PATH}/Dockerfile" "${NODE_ONLY}"

  # Ensure luxd and xsvm binaries are available to create an initial db state that includes subnets.
  echo "Building binaries required for configuring the ${TEST_SETUP} test setup"
  "${LUX_PATH}"/scripts/build.sh
  "${LUX_PATH}"/scripts/build_xsvm.sh

  echo "Generating compose configuration for ${TEST_SETUP}"
  gen_antithesis_compose_config "${IMAGE_TAG}" "${LUX_PATH}/tests/antithesis/xsvm/gencomposeconfig" \
                                "${LUX_PATH}/build/antithesis/xsvm" \
                                "LUXD_PATH=${LUX_PATH}/build/luxd AVAGO_PLUGIN_DIR=${LUX_PATH}/build/plugins"

  build_antithesis_images_for_luxd "${TEST_SETUP}" "${IMAGE_PREFIX}" "${LUX_PATH}/vms/example/xsvm/Dockerfile"
fi
