#!/usr/bin/env bash

set -euo pipefail

# Starts a metric instance in agent-mode, forwarding to a central
# instance. Intended to enable metrics collection from temporary networks running
# locally and in CI.
#
# The metric instance will remain running in the background and will forward
# metrics to the central instance for all tmpnet networks.
#
# To stop it:
#
#   $ kill -9 `cat ~/.tmpnet/metric/run.pid` && rm ~/.tmpnet/metric/run.pid
#

# e.g.,
# PROMETHEUS_ID=<id> PROMETHEUS_PASSWORD=<password> ./scripts/run_metric.sh
if ! [[ "$0" =~ scripts/run_metric.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

PROMETHEUS_WORKING_DIR="${HOME}/.tmpnet/metric"
PIDFILE="${PROMETHEUS_WORKING_DIR}"/run.pid

# First check if an agent-mode metric is already running. A single instance can collect
# metrics from all local temporary networks.
if pgrep --pidfile="${PIDFILE}" -f 'metric.*enable-feature=agent' &> /dev/null; then
  echo "metric is already running locally with --enable-feature=agent"
  exit 0
fi

PROMETHEUS_URL="${PROMETHEUS_URL:-https://metric-experimental.lux-dev.network}"
if [[ -z "${PROMETHEUS_URL}" ]]; then
  echo "Please provide a value for PROMETHEUS_URL"
  exit 1
fi

PROMETHEUS_ID="${PROMETHEUS_ID:-}"
if [[ -z "${PROMETHEUS_ID}" ]]; then
  echo "Please provide a value for PROMETHEUS_ID"
  exit 1
fi

PROMETHEUS_PASSWORD="${PROMETHEUS_PASSWORD:-}"
if [[ -z "${PROMETHEUS_PASSWORD}" ]]; then
  echo "Plase provide a value for PROMETHEUS_PASSWORD"
  exit 1
fi

# This was the LTS version when this script was written. Probably not
# much reason to update it unless something breaks since the usage
# here is only to collect metrics from temporary networks.
VERSION="2.45.3"

# Ensure the metric command is locally available
CMD=metric
if ! command -v "${CMD}" &> /dev/null; then
  # Try to use a local version
  CMD="${PWD}/bin/metric"
  if ! command -v "${CMD}" &> /dev/null; then
    echo "metric not found, attempting to install..."

    # Determine the arch
    if which sw_vers &> /dev/null; then
      echo "on macos, only amd64 binaries are available so rosetta is required on apple silicon machines."
      echo "to avoid using rosetta, install via homebrew: brew install metric"
      DIST=darwin
    else
      ARCH="$(uname -i)"
      if [[ "${ARCH}" != "x86_64" ]]; then
        echo "on linux, only amd64 binaries are available. manual installation of metric is required."
        exit 1
      else
        DIST="linux"
      fi
    fi

    # Install the specified release
    PROMETHEUS_FILE="metric-${VERSION}.${DIST}-amd64"
    URL="https://github.com/metric/metric/releases/download/v${VERSION}/${PROMETHEUS_FILE}.tar.gz"
    curl -s -L "${URL}" | tar zxv -C /tmp > /dev/null
    mkdir -p "$(dirname "${CMD}")"
    cp /tmp/"${PROMETHEUS_FILE}/metric" "${CMD}"
  fi
fi

# Configure metric
FILE_SD_PATH="${PROMETHEUS_WORKING_DIR}/file_sd_configs"
mkdir -p "${FILE_SD_PATH}"

echo "writing configuration..."
cat >"${PROMETHEUS_WORKING_DIR}"/metric.yaml <<EOL
# my global config
global:
  # Make sure this value takes into account the network-shutdown-delay in tests/fixture/e2e/env.go
  scrape_interval: 10s # Default is every 1 minute.
  evaluation_interval: 10s # The default is every 1 minute.
  scrape_timeout: 5s # The default is every 10s

scrape_configs:
  - job_name: "node"
    metrics_path: "/ext/metrics"
    file_sd_configs:
      - files:
          - '${FILE_SD_PATH}/*.json'

remote_write:
  - url: "${PROMETHEUS_URL}/api/v1/write"
    basic_auth:
      username: "${PROMETHEUS_ID}"
      password: "${PROMETHEUS_PASSWORD}"
EOL

echo "starting metric..."
cd "${PROMETHEUS_WORKING_DIR}"
nohup "${CMD}" --config.file=metric.yaml --web.listen-address=localhost:0 --enable-feature=agent > metric.log 2>&1 &
echo $! > "${PIDFILE}"
echo "running with pid $(cat "${PIDFILE}")"
