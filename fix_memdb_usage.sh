#!/bin/bash
# Fix memdb.New() usage in test files to use internal database package

echo "Fixing memdb.New() usage in test files..."

# Find all test files that use memdb.New()
find . -name "*_test.go" -type f -exec grep -l 'memdb\.New()' {} \; | while read file; do
    echo "Checking: $file"
    
    # Check if file imports external database/memdb
    if grep -q '"github.com/luxfi/database/memdb"' "$file"; then
        echo "  Fixing imports in: $file"
        # Replace external memdb import with internal
        sed -i 's|"github.com/luxfi/database/memdb"|"github.com/luxfi/node/database/memdb"|g' "$file"
    fi
done

# Also fix any regular files that might have the issue
find . -name "*.go" -type f ! -name "*_test.go" -exec grep -l '"github.com/luxfi/database/memdb"' {} \; | while read file; do
    echo "Fixing non-test file: $file"
    sed -i 's|"github.com/luxfi/database/memdb"|"github.com/luxfi/node/database/memdb"|g' "$file"
done

echo "memdb import fixes complete"