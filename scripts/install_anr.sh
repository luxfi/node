#!/usr/bin/env bash
set -e
set -o nounset
set -o pipefail

# Lux root directory
LUX_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

#################################
# download lux-network-runner
# https://github.com/luxdefi/lux-network-runner
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
NETWORK_RUNNER_VERSION=1.3.9
anr_workdir=${ANR_WORKDIR:-"/tmp"}
DOWNLOAD_PATH=${anr_workdir}/lux-network-runner-v${NETWORK_RUNNER_VERSION}.tar.gz
DOWNLOAD_URL="https://github.com/luxdefi/lux-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/lux-network-runner_${NETWORK_RUNNER_VERSION}_${GOOS}_${GOARCH}.tar.gz"
echo "Installing lux-network-runner ${NETWORK_RUNNER_VERSION} to ${anr_workdir}/lux-network-runner"

# download only if not already downloaded
if [ ! -f "$DOWNLOAD_PATH" ]; then
  echo "downloading lux-network-runner ${NETWORK_RUNNER_VERSION} at ${DOWNLOAD_URL} to ${DOWNLOAD_PATH}"
  curl --fail -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}
else
  echo "lux-network-runner ${NETWORK_RUNNER_VERSION} already downloaded at ${DOWNLOAD_PATH}"
fi

rm -f ${anr_workdir}/lux-network-runner

echo "extracting downloaded lux-network-runner"
tar xzvf ${DOWNLOAD_PATH} -C ${anr_workdir}
${anr_workdir}/lux-network-runner -h
