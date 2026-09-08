#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# Build the C++ evaluator against the C++ node ALREADY BUILT in place.
#
# The compile and link arguments are READ OUT of that build rather than
# restated: a copy of them here would be a second declaration of how this
# node is built, and it would be wrong the first time the node's dependencies
# moved. `set_root_test` is the target borrowed from, because it is the one
# whose object file is the same shape as this program's — it includes
# lux/node/validators.hpp and links libnode.a.
set -euo pipefail

node_dir=${1:?usage: build.sh <lux-cpp/node> <out>}
out=${2:?usage: build.sh <lux-cpp/node> <out>}
here=$(cd "$(dirname "$0")" && pwd)
build=$node_dir/build
borrow=$build/CMakeFiles/set_root_test.dir

if [ ! -f "$borrow/flags.make" ] || [ ! -f "$borrow/link.txt" ]; then
    echo "the C++ node is not built at $build — run 'make luxd-cpp' first" >&2
    exit 1
fi

includes=$(sed -n 's/^CXX_INCLUDES = //p' "$borrow/flags.make")
flags=$(sed -n 's/^CXX_FLAGS = //p' "$borrow/flags.make")

# The link line, with the borrowed target's object and output name replaced by
# this program's. Everything else — the library list and the rpath — is theirs.
link=$(sed \
    -e "s#CMakeFiles/set_root_test.dir/test/set_root_test.cpp.o#$out.o#" \
    -e "s#-o set_root_test#-o $out#" \
    "$borrow/link.txt")

# shellcheck disable=SC2086
c++ $flags $includes -c "$here/main.cpp" -o "$out.o"
cd "$build"
# shellcheck disable=SC2086
eval "$link"
