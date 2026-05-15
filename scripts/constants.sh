#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Where Lux Node binary goes
node_path="$NODE_PATH/build/luxd"

# Docker Hub repository for node images
node_dockerhub_repo="luxfi/node"

# Static compilation
static_ld_flags=''
if [ "${STATIC_COMPILATION:-}" = 1 ]
then
    export CC=musl-gcc
    which $CC > /dev/null || ( echo $CC must be available for static compilation && exit 1 )
    static_ld_flags=' -extldflags "-static" -linkmode external '
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
# Only set CGO_ENABLED if not already set (allows CGO_ENABLED=0 for cross-compilation)
export CGO_ENABLED="${CGO_ENABLED:-1}"

# Disable version control fallbacks
export GOPROXY="${GOPROXY:-https://proxy.golang.org}"

# Use GOPRIVATE to bypass Go proxy for luxfi packages (zip too large for proxy)
export GOPRIVATE="${GOPRIVATE:-github.com/luxfi/*}"
export GONOSUMDB="${GONOSUMDB:-github.com/luxfi/*}"

# Configure pkg-config path for C++ libraries (luxcpp)
# Searches for installed libraries in common locations
LUXCPP_ROOT="${LUXCPP_ROOT:-}"
if [ -z "$LUXCPP_ROOT" ]; then
    # Try to find luxcpp relative to this repo
    if [ -d "$NODE_PATH/../luxcpp/install/lib/pkgconfig" ]; then
        LUXCPP_ROOT="$NODE_PATH/../luxcpp/install"
    elif [ -d "$HOME/work/luxcpp/install/lib/pkgconfig" ]; then
        LUXCPP_ROOT="$HOME/work/luxcpp/install"
    fi
fi

if [ -n "$LUXCPP_ROOT" ] && [ -d "$LUXCPP_ROOT/lib/pkgconfig" ]; then
    export PKG_CONFIG_PATH="${LUXCPP_ROOT}/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
    export LD_LIBRARY_PATH="${LUXCPP_ROOT}/lib:${LD_LIBRARY_PATH:-}"
    export DYLD_LIBRARY_PATH="${LUXCPP_ROOT}/lib:${DYLD_LIBRARY_PATH:-}"
fi
