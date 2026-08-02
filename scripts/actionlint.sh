#!/usr/bin/env bash

set -eo pipefail

# With no arguments actionlint scans .github/workflows. In this repo that
# directory holds packaging scripts and release docs and NOT ONE yaml file — CI
# is native and lives in .hanzo/workflows. actionlint treats an empty scan as an
# error, so the gate failed with
#
#   no YAML file was found in "…/.github/workflows"
#   exit status 3
#
# for looking in the wrong place, not for anything wrong with a workflow.
#
# A directory is not accepted as an argument ("is a directory"), so the files are
# globbed. nullglob stops an unmatched pattern being passed through literally.
if [[ $# -eq 0 ]]; then
  shopt -s nullglob
  set -- .hanzo/workflows/*.yml .hanzo/workflows/*.yaml
  shopt -u nullglob
  if [[ $# -eq 0 ]]; then
    echo "no workflow yaml found under .hanzo/workflows" >&2
    exit 255
  fi
fi

go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.1 "$@"
