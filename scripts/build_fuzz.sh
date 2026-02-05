#!/usr/bin/env bash

# First argument is the time, in seconds, to run each fuzz test for.
# If not provided, defaults to 1 second.
#
# Second argument is the directory to run fuzz tests in.
# If not provided, defaults to the current directory.

set -euo pipefail

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

# Directory above this script
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

fuzzTime=${1:-1}
fuzzDir=${2:-.}

# Add 1 second buffer to avoid context deadline exceeded errors at exact timeout boundary
# Go's fuzzer can report failures when interrupted at exact deadline
# Apply buffer for any fuzz time > 1 second (covers the 2-second case used in CI)
actualFuzzTime=$((fuzzTime > 1 ? fuzzTime - 1 : fuzzTime))

files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' "$fuzzDir")
failed=false
for file in ${files}
do
    # Skip files that have build constraints requiring grpc (these won't build without -tags grpc)
    if head -5 "$file" | grep -q "//go:build.*grpc"; then
        echo "Skipping $file (requires grpc build tag)"
        continue
    fi
    # Use sed instead of grep -P for macOS compatibility
    funcs=$(sed -n 's/^func \(Fuzz[a-zA-Z0-9_]*\).*/\1/p' "$file")
    for func in ${funcs}
    do
        echo "Fuzzing $func in $file"
        parentDir=$(dirname "$file")
        # If any of the fuzz tests fail, return exit code 1
        if ! go test -tags test "$parentDir" -run="$func" -fuzz="$func" -fuzztime="${actualFuzzTime}"s; then
            failed=true
        fi
    done
done

if $failed; then
    exit 1
fi
