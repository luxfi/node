#!/bin/bash
# Comprehensive fix script to remove all snow* references

set -e

NODE_DIR="/Users/z/work/lux/node"
cd "$NODE_DIR"

echo "=== Fixing snow* references ==="

# Find all Go files with snow references (excluding vendor and disabled)
FILES=$(find . -name "*.go" -type f -not -path "*/vendor/*" -not -path "*/.git/*" -not -path "*/.*" | \
  xargs grep -l "snowman\|snowstorm" 2>/dev/null || true)

if [ -z "$FILES" ]; then
  echo "No files with snow* references found"
else
  for file in $FILES; do
    echo "Processing: $file"

    # Remove snowman/snowstorm imports
    sed -i '' '/import.*snowman/d' "$file"
    sed -i '' '/import.*snowstorm/d' "$file"
    sed -i '' '/".*snowman"/d' "$file"
    sed -i '' '/".*snowstorm"/d' "$file"

    # Replace snowman.Block with block.Block
    sed -i '' 's/snowman\.Block/block.Block/g' "$file"

    # Replace snowman.Consensus with engine types
    sed -i '' 's/snowman\.Consensus/engine.Consensus/g' "$file"

    # Replace other snowman types
    sed -i '' 's/snowman\.Engine/engine.Engine/g' "$file"
    sed -i '' 's/snowman\.Getter/getter.Getter/g' "$file"
    sed -i '' 's/snowstorm\.Consensus/dag.Consensus/g' "$file"

    # Add consensus imports if block.Block is used
    if grep -q "block\.Block" "$file" && ! grep -q "consensus/engine/chain/block" "$file"; then
      sed -i '' 's|"github.com/luxfi/consensus/engine/chain"|"github.com/luxfi/consensus/engine/chain"\n\t"github.com/luxfi/consensus/engine/chain/block"|' "$file"
    fi
  done
fi

echo "=== Fixing syncer references ==="
# Remove syncer package references (doesn't exist)
find . -name "*.go" -type f -not -path "*/vendor/*" | \
  xargs sed -i '' '/import.*syncer/d' 2>/dev/null || true

echo "=== Done ==="
