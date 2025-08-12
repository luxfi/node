#!/usr/bin/env bash

set -euo pipefail

# Directory above this script
NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Excluded targets - skip mock generation, proto files, and e2e tests for now
EXCLUDED_TARGETS="| grep -v /mocks | grep -v proto | grep -v tests/e2e | grep -v tests/load | grep -v tests/upgrade | grep -v tests/fixture"

# Get test targets
TEST_TARGETS="$(eval "go list ./... ${EXCLUDED_TARGETS}")"

# Run tests with race detection
# shellcheck disable=SC2086
go test -tags test -shuffle=on -race -timeout="${TIMEOUT:-120s}" -coverprofile="coverage.out" -covermode="atomic" ${TEST_TARGETS}