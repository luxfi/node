#!/usr/bin/env bash

# First argument is the time, in seconds, to run each fuzz test for.
# If not provided, defaults to 1 second.
#
# Second argument is the directory to run fuzz tests in.
# If not provided, defaults to the current directory.

set -euo pipefail

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

# Directory above this script
NODE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$NODE_PATH"/scripts/constants.sh

fuzzTime=${1:-1}
fuzzDir=${2:-.}

# Leave a fixed shutdown margin. Go's fuzzer may cross its requested fuzztime
# while workers finish an input; without this margin a healthy target can end
# with only "context deadline exceeded".
actualFuzzTime=$((fuzzTime > 5 ? fuzzTime - 5 : 1))

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
        # A worker can finish just after Go cancels the fuzz interval. Go then
        # returns only "context deadline exceeded" even though no input failed.
        # Retry that infrastructure-only result once; assertion failures,
        # panics, and reproducible fuzz inputs still fail immediately.
        if output=$(go test -tags test "$parentDir" -run="$func" -fuzz="$func" -fuzztime="${actualFuzzTime}"s 2>&1); then
            printf '%s\n' "$output"
        else
            printf '%s\n' "$output"
            if printf '%s\n' "$output" | grep -q '^ *context deadline exceeded$'; then
                echo "Retrying $func once after fuzz-worker shutdown exceeded its deadline"
                if ! go test -tags test "$parentDir" -run="$func" -fuzz="$func" -fuzztime="${actualFuzzTime}"s; then
                    failed=true
                fi
            else
                failed=true
            fi
        fi
    done
done

if $failed; then
    exit 1
fi
