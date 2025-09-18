#!/bin/bash

echo "Replacing consensus.Context with context.Context..."

# Find all Go files and replace consensus.Context with context.Context
find . -name "*.go" -type f | while read -r file; do
    # Check if file has consensus.Context
    if grep -q "\\*consensus\\.Context\\|consensus\\.Context" "$file" 2>/dev/null; then
        echo "Processing $file"

        # Replace *consensus.Context with context.Context
        sed -i 's/\*consensus\.Context/context.Context/g' "$file"

        # Replace consensus.Context with context.Context (where not preceded by *)
        sed -i 's/\([^*]\)consensus\.Context/\1context.Context/g' "$file"

        # Ensure context package is imported if not already
        if ! grep -q '"context"' "$file"; then
            # Add context import after package declaration
            sed -i '/^package /a\\nimport "context"' "$file"
        fi
    fi
done

# Also replace any ctx *consensus.Context variable declarations
find . -name "*.go" -type f -exec sed -i 's/ctx \*consensus\.Context/ctx context.Context/g' {} \;

# Replace ctx := &consensus.Context with proper context
find . -name "*.go" -type f -exec sed -i 's/ctx := &consensus\.Context/ctx := context.Background()/g' {} \;

echo "Complete!"