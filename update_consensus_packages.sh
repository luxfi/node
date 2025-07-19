#!/bin/bash

echo "Updating consensus package imports..."

# Update package declarations
find consensus/sampling -name "*.go" -type f -exec sed -i 's/^package snowball$/package sampling/g' {} \;
find consensus/dag -name "*.go" -type f -exec sed -i 's/^package lux$/package dag/g' {} \;
find consensus/dag -name "*.go" -type f -exec sed -i 's/^package snowstorm$/package dag/g' {} \;

# Update imports
find . -name "*.go" -type f | while read -r file; do
    # Skip vendor directories
    if [[ "$file" == *"/vendor/"* ]]; then
        continue
    fi
    
    # Update imports
    sed -i 's|"github.com/luxfi/node/consensus/snowball|"github.com/luxfi/node/consensus/sampling|g' "$file"
    sed -i 's|"github.com/luxfi/node/consensus/lux|"github.com/luxfi/node/consensus/dag|g' "$file"
    sed -i 's|"github.com/luxfi/node/consensus/snowstorm|"github.com/luxfi/node/consensus/dag|g' "$file"
done

echo "Package updates complete."