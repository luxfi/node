#!/bin/bash

# Simple POA build script
echo "Building LUX node with POA support..."

# Navigate to node directory
cd /home/z/node

# Build with standard Go build command, skipping problematic dependencies
echo "Building luxd binary..."
go build -tags="secp256k1" -o ./build/luxd ./main/main.go

if [ -f "./build/luxd" ]; then
    echo "Build successful! Binary located at: ./build/luxd"
    echo ""
    echo "To run with POA mode:"
    echo "./build/luxd --poa-mode-enabled --poa-single-node-mode --sybil-protection-disabled --sybil-protection-disabled-weight=1000000"
else
    echo "Build failed!"
    exit 1
fi