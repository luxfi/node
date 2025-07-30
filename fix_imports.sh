#!/bin/bash

# Fix all imports from external to internal database package
find . -name "*.go" -type f ! -path "./database/*" -exec grep -l '"github.com/luxfi/database"' {} \; | while read file; do
    echo "Fixing imports in: $file"
    sed -i 's|"github.com/luxfi/database"|"github.com/luxfi/node/database"|g' "$file"
    sed -i 's|"github.com/luxfi/database/|\t"github.com/luxfi/node/database/|g' "$file"
done

echo "Import fixes complete"