#!/usr/bin/env bash

set -euo pipefail

if [[ -f "flake.nix" ]]; then
  echo "Starting nix shell for local flake"
  FLAKE=
else
  echo "No local flake found, will attempt to use luxd flake"

  # Get module details from go.mod
  MODULE_DETAILS="$(go list -m "github.com/luxfi/node" 2>/dev/null)"

  # Extract the version part
  LUX_VERSION="$(echo "${MODULE_DETAILS}" | awk '{print $2}')"

  if [[ -z "${LUX_VERSION}" ]]; then
    echo "Failed to get luxd version from go.mod"
    exit 1
  fi

  # Check if the version matches the pattern where the last part is the module hash
  # v*YYYYMMDDHHMMSS-abcdef123456
  #
  # If not, the value is assumed to represent a tag
  if [[ "${LUX_VERSION}" =~ ^v.*[0-9]{14}-[0-9a-f]{12}$ ]]; then
    # Use the module hash as the version
    LUX_VERSION="$(echo "${LUX_VERSION}" | cut -d'-' -f3)"
  fi

  FLAKE="github:luxfi/node?ref=${LUX_VERSION}"
  echo "Starting nix shell for ${FLAKE}"
fi

nix develop "${FLAKE}" "${@}"
