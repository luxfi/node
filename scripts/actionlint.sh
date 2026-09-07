#!/usr/bin/env bash

set -eo pipefail

# With no arguments actionlint scans .github/workflows, and it treats an empty
# scan as an error rather than a pass:
#
#   no YAML file was found in "…/.github/workflows"
#   exit status 3
#
# The workflows moved from .hanzo/workflows to .github/workflows so that BOTH
# planes read them — github.com reads only the latter — so both paths are
# globbed here. Either one holding the yaml is a valid layout; only neither is
# an error, and that is a real one worth failing on.
#
# A directory is not accepted as an argument ("is a directory"), so the files are
# globbed. nullglob stops an unmatched pattern being passed through literally.
if [[ $# -eq 0 ]]; then
  shopt -s nullglob
  set -- .github/workflows/*.yml .github/workflows/*.yaml \
         .hanzo/workflows/*.yml .hanzo/workflows/*.yaml
  shopt -u nullglob
  if [[ $# -eq 0 ]]; then
    echo "no workflow yaml under .github/workflows or .hanzo/workflows" >&2
    exit 255
  fi
fi

go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.1 "$@"
