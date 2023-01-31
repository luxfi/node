#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Testing specific variables
lux_testing_repo="luxdefi/lux-testing"
node_byzantine_repo="luxdefi/lux-byzantine"

# Define lux-testing and lux-byzantine versions to use
lux_testing_image="luxdefi/lux-testing:master"
node_byzantine_image="luxdefi/lux-byzantine:master"

# Fetch the images
# If Docker Credentials are not available fail
if [[ -z ${DOCKER_USERNAME} ]]; then
    echo "Skipping Tests because Docker Credentials were not present."
    exit 1
fi

# Avalanche root directory
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd ../.. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

# Login to docker
echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin

# Receives params for debug execution
testBatch="${1:-}"
shift 1

echo "Running Test Batch: ${testBatch}"

# pulling the lux-testing image
docker pull $lux_testing_image
docker pull $node_byzantine_image

# Setting the build ID
git_commit_id=$( git rev-list -1 HEAD )

# Build current node
source "$AVALANCHE_PATH"/scripts/build_image.sh -r

# Target built version to use in lux-testing
lux_image="$node_dockerhub_repo:$current_branch"

echo "Execution Summary:"
echo ""
echo "Running Avalanche Image: ${lux_image}"
echo "Running Avalanche Image Tag: $current_branch"
echo "Running Avalanche Testing Image: ${lux_testing_image}"
echo "Running Avalanche Byzantine Image: ${node_byzantine_image}"
echo "Git Commit ID : ${git_commit_id}"
echo ""

# >>>>>>>> lux-testing custom parameters <<<<<<<<<<<<<
custom_params_json="{
    \"isKurtosisCoreDevMode\": false,
    \"nodeImage\":\"${lux_image}\",
    \"nodeByzantineImage\":\"${node_byzantine_image}\",
    \"testBatch\":\"${testBatch}\"
}"
# >>>>>>>> lux-testing custom parameters <<<<<<<<<<<<<

bash "$AVALANCHE_PATH/.kurtosis/kurtosis.sh" \
    --custom-params "${custom_params_json}" \
    ${1+"${@}"} \
    "${lux_testing_image}"
