#!/bin/bash
set -e

echo "Building luxd without problematic dependencies..."

# Build with minimal features
go build \
  -tags "noblst" \
  -ldflags "-s -w" \
  -o build/luxd \
  ./main/

echo "Build complete: build/luxd"