#!/bin/bash
# Comprehensive fix for consensus package imports and references

set -e

cd /Users/z/work/lux/node

echo "=== Fixing consensus imports and references ==="

# Find all Go files that need fixing (excluding vendor, .git, disabled)
FILES=$(find . -name "*.go" -type f -not -path "*/vendor/*" -not -path "*/.git/*" -not -name "*_test.go" | \
  xargs grep -l "snow\\.Context\|common\\.VM\|common\\.Fx\|common\\.AppSender\|common\\.Message\|snowstorm\|snowball\|chain\\.Block" 2>/dev/null || true)

for file in $FILES; do
  echo "Processing: $file"

  # Backup file
  cp "$file" "$file.bak"

  # Fix duplicate core imports - remove the shorter one
  sed -i '' '/^[[:space:]]*"github.com\/luxfi\/consensus\/core"$/d' "$file"

  # Add consensus package import if common.* or consensus.Fx is used
  if grep -q "common\\.VM\|common\\.Fx\|consensus\\.Fx" "$file" && ! grep -q '"github.com/luxfi/consensus"' "$file"; then
    sed -i '' 's|^\([[:space:]]*\)"github.com/luxfi/node/database"|&\n\1"github.com/luxfi/consensus"|' "$file"
  fi

  # Add appsender import if common.AppSender is used
  if grep -q "common\\.AppSender\|appsender\\.AppSender" "$file" && ! grep -q '"github.com/luxfi/consensus/core/appsender"' "$file"; then
    sed -i '' 's|^\([[:space:]]*\)"github.com/luxfi/node/database"|&\n\1"github.com/luxfi/consensus/core/appsender"|' "$file"
  fi

  # Add context import if snow.Context is used
  if grep -q "snow\\.Context\|\\*snow\\.Context" "$file" && ! grep -q 'consensusctx "github.com/luxfi/consensus/context"' "$file"; then
    sed -i '' 's|^\([[:space:]]*\)"github.com/luxfi/node/database"|&\n\1consensusctx "github.com/luxfi/consensus/context"|' "$file"
  fi

  # Add block import if block.Block is not imported but chain.Block or block.ChainVM is used
  if grep -q "chain\\.Block\|block\\.ChainVM" "$file" && ! grep -q '"github.com/luxfi/consensus/engine/chain/block"' "$file"; then
    sed -i '' 's|^\([[:space:]]*\)"github.com/luxfi/consensus/engine/chain"|&\n\1"github.com/luxfi/consensus/engine/chain/block"|' "$file"
  fi

  # Replace snow.Context with consensusctx.Context
  sed -i '' 's/\*snow\.Context/*consensusctx.Context/g' "$file"
  sed -i '' 's/snow\.Context/consensusctx.Context/g' "$file"

  # Replace snow.State with core.State
  sed -i '' 's/snow\.State/core.State/g' "$file"

  # Replace common.VM with core.VM
  sed -i '' 's/common\.VM/core.VM/g' "$file"

  # Replace common.Fx with consensus.Fx
  sed -i '' 's/common\.Fx/consensus.Fx/g' "$file"
  sed -i '' 's/\[\]\*common\.Fx/[]*consensus.Fx/g' "$file"

  # Replace common.AppSender with appsender.AppSender
  sed -i '' 's/common\.AppSender/appsender.AppSender/g' "$file"

  # Replace common.Message with block.Message
  sed -i '' 's/common\.Message/block.Message/g' "$file"

  # Replace chain.Block with block.Block (only in type declarations and returns)
  sed -i '' 's/\(func.*\) chain\.Block/\1 block.Block/g' "$file"
  sed -i '' 's/\(return.*\) chain\.Block/\1 block.Block/g' "$file"
  sed -i '' 's/(chain\.Block/(block.Block/g' "$file"
  sed -i '' 's/)chain\.Block/)block.Block/g' "$file"

  # Remove snowstorm references
  sed -i '' '/snowstorm\.Tx/d' "$file"
  sed -i '' 's/_ snowstorm\./_ /g' "$file"

  # Remove snowball references
  sed -i '' 's/snowball\./consensus./g' "$file"

  # Clean up any import statements that are now unused
  # (This is basic - goimports will do a better job)

done

echo "=== Running goimports to clean up imports ==="
find . -name "*.go" -type f -not -path "*/vendor/*" -not -path "*/.git/*" -exec goimports -w {} \; 2>/dev/null || true

echo "=== Done ==="
echo "Backup files saved with .bak extension"
