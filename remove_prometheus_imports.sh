#!/bin/bash
# Remove prometheus imports that are not needed

find . -name "*.go" -type f | while read file; do
    # Skip vendor and .git directories
    if [[ "$file" == *"vendor"* ]] || [[ "$file" == *".git"* ]]; then
        continue
    fi
    
    # Remove prometheus import lines
    sed -i '/^\s*"github\.com\/prometheus\/client_golang\/prometheus"/d' "$file"
    
    # Clean up empty import groups
    sed -i '/^import ($/,/^)$/{/^import ($/n;/^)$/!b;N;s/import (\n)/import ()/}' "$file"
done
echo "Removed prometheus imports"
