#!/bin/bash

echo "Fixing avalanchego imports to use luxfi/node..."

# Find all Go files and replace the import
find . -name "*.go" -type f -exec sed -i '' 's|github.com/ava-labs/avalanchego|github.com/luxfi/node|g' {} \;

echo "Import replacement complete!"

# Show a sample of the changes
echo "Sample of updated files:"
find . -name "*.go" -type f -exec grep -l "github.com/luxfi/node" {} \; | head -5