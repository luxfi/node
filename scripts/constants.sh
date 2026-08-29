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

# The Go experiments every build and test of this repo runs with.
#
# This is the only place the repo picks them, so `make`, scripts/build.sh and CI
# cannot disagree about what they are compiling.
#
# jsonv2 is what the released image is built with. runtimesecret is NOT usable
# here: luxfi/crypto's cgo BLS path calls blst_keygen from inside
# runtime/secret.Do, and a cgo transition out of a secret context faults in
# runtime.asmcgocall. luxd then takes a SIGSEGV in config.getStakingSigner
# before it writes its first log line, with no Go traceback to say why. It needs
# only cgo, which is this repo's default, so it is not specific to any kernel.
export GOEXPERIMENT="${GOEXPERIMENT:-jsonv2}"

# An explicit override must not bring that crash back silently.
if [[ "${GOEXPERIMENT}" == *runtimesecret* && "${CGO_ENABLED}" != "0" ]]; then
    echo "ERROR: GOEXPERIMENT=runtimesecret with CGO_ENABLED=${CGO_ENABLED} builds a luxd that" >&2
    echo "       SIGSEGVs on startup (cgo BLS keygen inside runtime/secret.Do)." >&2
    echo "       Build with CGO_ENABLED=0 to take the pure-Go BLS path, or drop runtimesecret." >&2
    exit 1
fi

# Disable version control fallbacks
export GOPROXY="${GOPROXY:-https://proxy.golang.org}"

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
