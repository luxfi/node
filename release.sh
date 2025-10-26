#!/bin/bash
set -e

echo "==================================="
echo "Lux Node Release Build v0.1.0-lux.18"
echo "==================================="

# Build
echo "Building binary..."
./scripts/build.sh

# Run quick tests
echo "Running quick tests..."
go test -timeout=30s ./ids/... ./utils/... ./codec/... 

echo "==================================="
echo "Build successful!"
echo "Binary: ./build/luxd"
echo "==================================="

# Package
echo "Creating release package..."
mkdir -p release
cp build/luxd release/luxd-linux-amd64
tar -czf release/luxd-v0.1.0-lux.18-linux-amd64.tar.gz -C release luxd-linux-amd64

echo "Release package created: release/luxd-v0.1.0-lux.18-linux-amd64.tar.gz"