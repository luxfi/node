#!/bin/bash
set -e

echo "Building luxd..."

# Skip dependency checking and just build
export GO111MODULE=on
export CGO_ENABLED=1

# Build with minimal dependencies
go build -ldflags="-s -w" -o build/luxd ./main

echo "Build complete!"
ls -la build/