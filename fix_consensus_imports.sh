#!/bin/bash

# Fix incorrect consensus imports
echo "Fixing consensus imports..."

# Replace imports that should point to local packages
find . -name "*.go" -type f ! -path "./vendor/*" -exec sed -i '' 's|"github.com/luxfi/consensus/|"github.com/luxfi/node/consensus/|g' {} \;

echo "Done fixing consensus imports"