#!/bin/bash

# Update all imports from github.com/luxfi/node to github.com/luxfi/node/v2

echo "Updating imports to v2..."

# Find all Go files and update imports
find . -name "*.go" -type f -not -path "./vendor/*" -not -path "./.git/*" | while read -r file; do
    # Use sed to replace the import paths
    sed -i '' 's|"github.com/luxfi/node/|"github.com/luxfi/node/v2/|g' "$file"
done

echo "Import update complete!"

# Update go.mod replace directives if any
if grep -q "github.com/luxfi/node " go.mod; then
    echo "Updating go.mod replace directives..."
    sed -i '' 's|github.com/luxfi/node |github.com/luxfi/node/v2 |g' go.mod
fi

echo "Done!"