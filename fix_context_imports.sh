#!/bin/bash

# Function to add context import to a file
add_context_import() {
    local file="$1"
    
    # Check if context is already imported
    if grep -q "^import.*context" "$file" || grep -q '"context"' "$file"; then
        echo "Skipping $file - context already imported"
        return
    fi
    
    # Check if file needs context
    if ! grep -q "context\." "$file"; then
        echo "Skipping $file - doesn't use context"
        return
    fi
    
    echo "Adding context import to $file"
    
    # Check if there's already an import block
    if grep -q "^import (" "$file"; then
        # Add context as first import in the block
        sed -i '/^import (/a\\t"context"' "$file"
    else
        # Find package declaration and add import after it
        sed -i '/^package /a\\nimport "context"' "$file"
    fi
}

# Process platformvm txs
echo "Processing platformvm/txs..."
for file in vms/platformvm/txs/*.go; do
    add_context_import "$file"
done

# Process xvm txs
echo "Processing xvm/txs..."
for file in vms/xvm/txs/*.go; do
    add_context_import "$file"
done

echo "Done!"