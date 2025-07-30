#!/bin/bash
# Fix remaining database imports

echo "Fixing remaining database imports..."

# Fix leveldb and pebbledb imports
find . -name "*.go" -type f -exec grep -l '"github.com/luxfi/database/leveldb"' {} \; | while read file; do
    echo "Fixing leveldb import in: $file"
    sed -i 's|"github.com/luxfi/database/leveldb"|"github.com/luxfi/node/database/leveldb"|g' "$file"
done

find . -name "*.go" -type f -exec grep -l '"github.com/luxfi/database/pebbledb"' {} \; | while read file; do
    echo "Fixing pebbledb import in: $file"
    sed -i 's|"github.com/luxfi/database/pebbledb"|"github.com/luxfi/node/database/pebbledb"|g' "$file"
done

# Fix prefixdb imports
find . -name "*.go" -type f -exec grep -l '"github.com/luxfi/database/prefixdb"' {} \; | while read file; do
    echo "Fixing prefixdb import in: $file"
    sed -i 's|"github.com/luxfi/database/prefixdb"|"github.com/luxfi/node/database/prefixdb"|g' "$file"
done

# Fix mockgen comments (just for clarity, doesn't affect functionality)
find . -name "mock_*.go" -type f -exec grep -l 'github.com/luxfi/database' {} \; | while read file; do
    echo "Fixing mockgen comments in: $file"
    sed -i 's|github.com/luxfi/database|github.com/luxfi/node/database|g' "$file"
done

echo "Import fixes complete"