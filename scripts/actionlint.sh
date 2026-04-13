#!/usr/bin/env bash

set -eo pipefail

go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.1 "$@"
