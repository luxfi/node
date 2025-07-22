#!/usr/bin/env bash

set -euo pipefail

# Run specific test packages that are failing
echo "Running confidence tests..."
go test -v ./consensus/confidence -run TestBinaryConfidence

echo "Running wrappers tests..."
go test -v ./utils/wrappers -run TestPacker

echo "Running genesis tests..."
go test -v ./genesis -run TestLUXAssetID