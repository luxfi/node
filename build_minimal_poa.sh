#!/bin/bash

# Minimal POA build script - builds only core components with POA support
echo "Building minimal LUX node with POA support..."

# Navigate to node directory
cd /home/z/node

# Set build flags to exclude problematic packages
export CGO_ENABLED=1
export CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"

# Build main package only with necessary tags
echo "Building luxd binary (minimal)..."
go build -tags="secp256k1" \
    -ldflags="-s -w" \
    -o ./build/luxd-poa \
    ./main/main.go

if [ -f "./build/luxd-poa" ]; then
    echo "Build successful! Binary located at: ./build/luxd-poa"
    echo ""
    echo "To run with POA mode:"
    echo "./build/luxd-poa --poa-mode-enabled --poa-single-node-mode --sybil-protection-disabled"
else
    echo "Build failed!"
    exit 1
fi