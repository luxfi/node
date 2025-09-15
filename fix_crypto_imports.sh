#!/bin/bash

# Replace node/utils/crypto/bls imports with luxfi/crypto/bls
find . -name "*.go" -type f | while read file; do
    # Skip backup files
    if [[ "$file" == *.backup ]]; then
        continue
    fi
    
    # Check if file contains the old import
    if grep -q "github.com/luxfi/node/utils/crypto/bls" "$file"; then
        echo "Updating: $file"
        sed -i 's|"github.com/luxfi/node/utils/crypto/bls"|"github.com/luxfi/crypto/bls"|g' "$file"
    fi
    
    # Also update any other crypto imports
    if grep -q "github.com/luxfi/node/utils/crypto" "$file"; then
        echo "Updating crypto imports in: $file"
        sed -i 's|"github.com/luxfi/node/utils/crypto"|"github.com/luxfi/crypto"|g' "$file"
    fi
done

echo "Done updating crypto imports"
