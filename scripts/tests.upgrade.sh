#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.upgrade.sh 1.7.16
# LUXGO_PATH=./path/to/node ./scripts/tests.upgrade.sh 1.7.16 # Customization of node path
if ! [[ "$0" =~ scripts/tests.upgrade.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "Missing version argument!"
  echo "Usage: ${0} [VERSION]" >>/dev/stderr
  exit 255
fi

LUXGO_PATH="$(realpath ${LUXGO_PATH:-./build/node})"

#################################
# download node
# https://github.com/luxdefi/node/releases
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
DOWNLOAD_URL=https://github.com/luxdefi/node/releases/download/v${VERSION}/node-linux-${GOARCH}-v${VERSION}.tar.gz
DOWNLOAD_PATH=/tmp/node.tar.gz
if [[ ${GOOS} == "darwin" ]]; then
  DOWNLOAD_URL=https://github.com/luxdefi/node/releases/download/v${VERSION}/node-macos-v${VERSION}.zip
  DOWNLOAD_PATH=/tmp/node.zip
fi

rm -f ${DOWNLOAD_PATH}
rm -rf /tmp/node-v${VERSION}
rm -rf /tmp/node-build

echo "downloading node ${VERSION} at ${DOWNLOAD_URL}"
curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded node"
if [[ ${GOOS} == "linux" ]]; then
  tar xzvf ${DOWNLOAD_PATH} -C /tmp
elif [[ ${GOOS} == "darwin" ]]; then
  unzip ${DOWNLOAD_PATH} -d /tmp/node-build
  mv /tmp/node-build/build /tmp/node-v${VERSION}
fi
find /tmp/node-v${VERSION}

# Sourcing constants.sh ensures that the necessary CGO flags are set to
# build the portable version of BLST. Without this, ginkgo may fail to
# build the test binary if run on a host (e.g. github worker) that lacks
# the instructions to build non-portable BLST.
source ./scripts/constants.sh

#################################
echo "building upgrade.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/upgrade
./tests/upgrade/upgrade.test --help

#################################
# By default, it runs all upgrade test cases!
echo "running upgrade tests against the local cluster with ${LUXGO_PATH}"
./tests/upgrade/upgrade.test \
  --ginkgo.v \
  --node-path=/tmp/node-v${VERSION}/node \
  --node-path-to-upgrade-to=${LUXGO_PATH}
