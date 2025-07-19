#!/bin/bash

echo "Updating final consensus package imports..."

# Update all imports
find . -name "*.go" -type f | while read -r file; do
    # Skip vendor directories
    if [[ "$file" == *"/vendor/"* ]]; then
        continue
    fi
    
    # Update snowtest to consensustest
    sed -i 's|"github.com/luxfi/node/consensus/snowtest|"github.com/luxfi/node/consensus/consensustest|g' "$file"
    
    # Fix any consensus/consensus paths from earlier
    sed -i 's|consensus/consensus/|consensus/|g' "$file"
done

echo "Final import updates complete."