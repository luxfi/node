#!/bin/bash

# Build LUX node with POA support
echo "Building LUX node with POA support..."

# Set up Go environment
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
# Navigate to node directory (parent of scripts)
cd "$SCRIPT_DIR/.."

# Build the node
echo "Running build script..."
./scripts/build.sh

# Check if build was successful
if [ -f "./build/luxd" ]; then
    echo "Build successful! Binary located at: ./build/luxd"
    echo ""
    echo "To run with POA mode:"
    echo "./build/luxd --poa-mode-enabled --poa-single-node-mode --sybil-protection-disabled --sybil-protection-disabled-weight=1000000"
else
    echo "Build failed!"
    exit 1
fi