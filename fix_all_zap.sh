#!/bin/bash
# Fix duplicate log imports
for file in api/info/service.go api/admin/service.go indexer/index.go indexer/indexer.go vms/platformvm/state/state.go; do
  # Remove duplicate log imports
  sed -i '/github.com\/luxfi\/log/,+0{/github.com\/luxfi\/log/d;}' "$file"
  # Replace zap references
  sed -i 's/zap\./log./g' "$file"
done
