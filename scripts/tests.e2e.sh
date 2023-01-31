#!/usr/bin/env bash
set -e
set -o nounset
set -o pipefail

# e.g.,
# ./scripts/build.sh
# ./scripts/tests.e2e.sh ./build/node
# ENABLE_WHITELIST_VTX_TESTS=true ./scripts/tests.e2e.sh ./build/node
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

AVALANCHEGO_PATH="${1-}"
if [[ -z "${AVALANCHEGO_PATH}" ]]; then
  echo "Missing AVALANCHEGO_PATH argument!"
  echo "Usage: ${0} [AVALANCHEGO_PATH]" >> /dev/stderr
  exit 255
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

ENABLE_WHITELIST_VTX_TESTS=${ENABLE_WHITELIST_VTX_TESTS:-false}
# ref. https://onsi.github.io/ginkgo/#spec-labels
GINKGO_LABEL_FILTER="!whitelist-tx"
if [[ ${ENABLE_WHITELIST_VTX_TESTS} == true ]]; then
  # run only "whitelist-tx" tests, no other test
  GINKGO_LABEL_FILTER="whitelist-tx"
fi
echo GINKGO_LABEL_FILTER: ${GINKGO_LABEL_FILTER}

#################################
# download netrunner
# https://github.com/luxdefi/netrunner
# TODO: migrate to upstream netrunner
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
NETWORK_RUNNER_VERSION=1.3.5-rc.0
DOWNLOAD_PATH=/tmp/netrunner.tar.gz
DOWNLOAD_URL="https://github.com/luxdefi/netrunner/releases/download/v${NETWORK_RUNNER_VERSION}/netrunner_${NETWORK_RUNNER_VERSION}_${GOOS}_${GOARCH}.tar.gz"

rm -f ${DOWNLOAD_PATH}
rm -f /tmp/netrunner

echo "downloading netrunner ${NETWORK_RUNNER_VERSION} at ${DOWNLOAD_URL} to ${DOWNLOAD_PATH}"
curl --fail -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

echo "extracting downloaded netrunner"
tar xzvf ${DOWNLOAD_PATH} -C /tmp
/tmp/netrunner -h

GOPATH="$(go env GOPATH)"
PATH="${GOPATH}/bin:${PATH}"

#################################
echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

#################################
# run "netrunner" server
echo "launch netrunner in the background"
/tmp/netrunner \
server \
--log-level debug \
--port=":12342" \
--disable-grpc-gateway &
PID=${!}

#################################
echo "running e2e tests against the local cluster with ${AVALANCHEGO_PATH}"
./tests/e2e/e2e.test \
--ginkgo.v \
--log-level debug \
--network-runner-grpc-endpoint="0.0.0.0:12342" \
--network-runner-node-path=${AVALANCHEGO_PATH} \
--network-runner-node-log-level="WARN" \
--test-keys-file=tests/test.insecure.secp256k1.keys --ginkgo.label-filter="${GINKGO_LABEL_FILTER}" \
&& EXIT_CODE=$? || EXIT_CODE=$?

kill ${PID}

if [[ ${EXIT_CODE} -gt 0 ]]; then
  echo "FAILURE with exit code ${EXIT_CODE}"
  exit ${EXIT_CODE}
else
  echo "ALL SUCCESS!"
fi
