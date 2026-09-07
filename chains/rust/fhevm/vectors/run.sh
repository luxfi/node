#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause-Eco
# Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
#
# Regenerates vectors.json from the REFERENCE Go package.
#
# The generator has to live inside `github.com/luxfi/chains/fhevm` to reach what
# it pins, and the reference checkout is shared, so this copies the package and
# the generator into a scratch directory and runs it there. Nothing is written to
# the reference.
#
#   ./run.sh [path-to-chains-checkout]
#
# The vectors are checked in: this only needs running when the Go chain changes,
# and when it does, a Rust test failing afterwards is the point.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
chains="${1:-$HOME/work/lux/chains}"
src="$chains/fhevm"

if [ ! -d "$src" ]; then
  echo "no reference package at $src" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cp -R "$chains" "$work/chains"
cp "$here/zz_gen_vectors_test.go" "$work/chains/fhevm/"

cd "$work/chains/fhevm"
GOWORK=off go test -count=1 -run TestWriteVectors -v . 2>&1 | tail -20
cp vectors.json "$here/vectors.json"
echo "wrote $here/vectors.json"
