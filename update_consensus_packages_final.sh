#!/bin/bash

echo "Updating consensus package declarations and imports..."

# Update package declarations
find consensus/sampling -name "*.go" -type f -exec sed -i 's/^package sampling$/package sampling/g' {} \;
find consensus/threshold -name "*.go" -type f -exec sed -i 's/^package quorum$/package threshold/g' {} \;
find consensus/confidence -name "*.go" -type f -exec sed -i 's/^package confidence$/package confidence/g' {} \;
find consensus/dag -name "*.go" -type f -exec sed -i 's/^package dag$/package dag/g' {} \;
find consensus/chain -name "*.go" -type f -exec sed -i 's/^package chain$/package chain/g' {} \;
find consensus/bft -name "*.go" -type f -exec sed -i 's/^package bft$/package bft/g' {} \;

# Update imports throughout the codebase
find . -name "*.go" -type f | while read -r file; do
    # Skip vendor directories
    if [[ "$file" == *"/vendor/"* ]]; then
        continue
    fi
    
    # Update imports
    sed -i 's|"github.com/luxfi/node/consensus/quorum|"github.com/luxfi/node/consensus/threshold|g' "$file"
done

echo "Package updates complete."