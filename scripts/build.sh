#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS] [BUILD_ARGS]

  Build node

  Options:

    -r  Build with race detector
    -p  Build profile (default: minimal)
        Profiles:
          minimal  - ZAP transport, all VMs, stripped (~30MB)
          core     - ZAP, P/X/C only, stripped (~25MB) [plugin-ready]
          full     - gRPC+ZAP, all features, stripped (~36MB)
          dev      - ZAP, all VMs, debug symbols (~40MB)
          tiny     - all VMs + UPX compressed (~20MB)

  BUILD_ARGS: Additional arguments to pass to go build (e.g., -tags purego)

  Examples:
    ./scripts/build.sh              # minimal stripped build
    ./scripts/build.sh -p full      # full build with all VMs and gRPC
    ./scripts/build.sh -p dev       # development build with debug symbols
    ./scripts/build.sh -p tiny      # smallest binary (UPX compressed)
    ./scripts/build.sh -r           # minimal with race detector
"
}

race=''
profile='minimal'
while getopts 'rp:' flag; do
  case "${flag}" in
    r)
      echo "Building with race detection enabled"
      race='-race'
      ;;
    p)
      profile="${OPTARG}"
      ;;
    *) print_usage
      exit 1 ;;
  esac
done

# Shift to get remaining arguments (build tags, etc.)
shift $((OPTIND-1))

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Configure the build environment
source "${REPO_ROOT}"/scripts/constants.sh
# Determine the git commit hash to use for the build
source "${REPO_ROOT}"/scripts/git_commit.sh

# Configure build based on profile
tags=""
ldflags="-X github.com/luxfi/node/version.GitCommit=$git_commit \
         -X github.com/luxfi/node/version.VersionMajor=$version_major \
         -X github.com/luxfi/node/version.VersionMinor=$version_minor \
         -X github.com/luxfi/node/version.VersionPatch=$version_patch \
         $static_ld_flags"
strip_flags=""
upx_compress=false

case "${profile}" in
  minimal)
    echo "Profile: minimal (ZAP, all VMs, NAT, stripped)"
    tags="allvms,nattraversal"
    strip_flags="-s -w"
    ;;
  core)
    echo "Profile: core (ZAP, P/X/C chains only, stripped)"
    # No optional VMs - future plugin-ready build
    strip_flags="-s -w"
    ;;
  full)
    echo "Profile: full (gRPC+ZAP, all VMs, all features, stripped)"
    tags="grpc,allvms,nattraversal,zxcvbn,metrics"
    strip_flags="-s -w"
    ;;
  dev)
    echo "Profile: dev (ZAP, core VMs, debug symbols)"
    # No strip flags, keep debug symbols
    ;;
  tiny)
    echo "Profile: tiny (all VMs, NAT + UPX compressed)"
    tags="allvms,nattraversal"
    strip_flags="-s -w"
    upx_compress=true
    ;;
  *)
    echo "Unknown profile: ${profile}"
    print_usage
    exit 1
    ;;
esac

# Apply strip flags to ldflags
if [ -n "$strip_flags" ]; then
  ldflags="$strip_flags $ldflags"
fi

# Build tags argument
tags_arg=""
if [ -n "$tags" ]; then
  tags_arg="-tags=$tags"
fi

# Enable Go 1.26 experimental features if not already set
export GOEXPERIMENT="${GOEXPERIMENT:-runtimesecret}"

echo "Building Lux Node v${version_major}.${version_minor}.${version_patch} with [$(go version)] GOEXPERIMENT=${GOEXPERIMENT}..."
go build -trimpath ${race} ${tags_arg} "$@" -o "${node_path}" \
   -ldflags "$ldflags" \
   "${REPO_ROOT}"/main

# Apply UPX compression if requested
if [ "$upx_compress" = true ]; then
  if command -v upx &> /dev/null; then
    echo "Compressing with UPX..."
    upx_flags="--best --lzma"
    if [[ "$OSTYPE" == "darwin"* ]]; then
      upx_flags="$upx_flags --force-macos"
    fi
    # shellcheck disable=SC2086
    upx $upx_flags "${node_path}"
  else
    echo "Warning: UPX not found, skipping compression"
    echo "Install with: brew install upx (macOS) or apt install upx (Linux)"
  fi
fi

# Show binary size
echo ""
echo "Binary: ${node_path}"
# shellcheck disable=SC2012
ls -lh "${node_path}" | awk '{print "Size:   " $5}'
