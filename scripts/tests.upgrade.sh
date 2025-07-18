#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.upgrade.sh                                               # Use default version
# ./scripts/tests.upgrade.sh 1.11.0                                        # Specify a version
# LUXD_PATH=./path/to/luxd ./scripts/tests.upgrade.sh 1.11.0 # Customization of luxd path
if ! [[ "$0" =~ scripts/tests.upgrade.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# The Lux local network does not support long-lived
# backwards-compatible networks. When a breaking change is made to the
# local network, this flag must be updated to the last compatible
# version with the latest code.
#
# v1.13.0 is the earliest version that supports Fortuna.
DEFAULT_VERSION="1.13.0"

VERSION="${1:-${DEFAULT_VERSION}}"
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >>/dev/stderr
  exit 255
fi

LUXD_PATH="$(realpath "${LUXD_PATH:-./build/luxd}")"

#################################
# download luxd
# https://github.com/luxfi/node/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION}/luxd-linux-${GOARCH}-v${VERSION}.tar.gz
DOWNLOAD_PATH=/tmp/luxd.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION}/luxd-macos-v${VERSION}.zip
  DOWNLOAD_PATH=/tmp/luxd.zip
fi

rm -f ${DOWNLOAD_PATH}
rm -rf "/tmp/luxd-v${VERSION}"
rm -rf /tmp/luxd-build

echo "downloading luxd ${VERSION} at ${DOWNLOAD_URL}"
curl -L "${DOWNLOAD_URL}" -o "${DOWNLOAD_PATH}"

echo "extracting downloaded luxd"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/luxd-build
  mv /tmp/luxd-build/build "/tmp/luxd-v${VERSION}"
fi
find "/tmp/luxd-v${VERSION}"

# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

#################################
# By default, it runs all upgrade test cases!
echo "running upgrade tests against the local cluster with ${LUXD_PATH}"
./bin/ginkgo -v ./tests/upgrade -- \
  --luxd-path="/tmp/luxd-v${VERSION}/luxd" \
  --luxd-path-to-upgrade-to="${LUXD_PATH}"
